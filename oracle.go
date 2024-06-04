package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm/utils"

	// _ "github.com/godror/godror"
	_ "github.com/sijms/go-ora/v2"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

const RowNumberAliasForOracle11 = "ROW_NUM"

type Config struct {
	DriverName        string
	DSN               string
	Conn              gorm.ConnPool //*sql.DB
	DefaultStringSize uint
	DBName            string
	DBVer             string
}

type Dialector struct {
	*Config
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{Config: &Config{DSN: dsn}}
}

func New(config Config) gorm.Dialector {
	return &Dialector{Config: &config}
}

func (d Dialector) DummyTableName() string {
	return "DUAL"
}

func (d Dialector) Name() string {
	return "oracle"
}

func (d Dialector) Initialize(db *gorm.DB) (err error) {

	db.NamingStrategy = Namer{
		NamingStrategy: db.NamingStrategy,
		DBName:         d.DBName,
	}
	d.DefaultStringSize = 1024

	// register callbacks
	//callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{WithReturning: true})
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		CreateClauses: []string{"INSERT", "VALUES", "ON CONFLICT", "RETURNING"},
		UpdateClauses: []string{"UPDATE", "SET", "WHERE", "RETURNING"},
		DeleteClauses: []string{"DELETE", "FROM", "WHERE", "RETURNING"},
	})

	// d.DriverName = "godror"
	d.DriverName = "oracle"

	// godror.Batch

	if d.Conn != nil {
		db.ConnPool = d.Conn
	} else {
		db.ConnPool, err = sql.Open(d.DriverName, d.DSN)
		if err != nil {
			return
		}
	}
	err = db.ConnPool.QueryRowContext(context.Background(), "select version from product_component_version where rownum = 1").Scan(&d.DBVer)
	if err != nil {
		return err
	}

	if err = db.Callback().Create().Replace("gorm:create", Create); err != nil {
		return
	}

	for k, v := range d.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}
	return
}

func (d Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	dbver, _ := strconv.Atoi(strings.Split(d.DBVer, ".")[0])
	if dbver > 0 && dbver < 12 {
		return map[string]clause.ClauseBuilder{
			"LIMIT": d.RewriteLimit11,
		}

	} else {
		return map[string]clause.ClauseBuilder{
			"LIMIT": d.RewriteLimit,
		}
	}
}

func (d Dialector) RewriteLimit(c clause.Clause, builder clause.Builder) {
	if limit, ok := c.Expression.(clause.Limit); ok {
		if stmt, ok := builder.(*gorm.Statement); ok {
			if _, ok := stmt.Clauses["ORDER BY"]; !ok {
				s := stmt.Schema
				builder.WriteString("ORDER BY ")
				if s != nil && s.PrioritizedPrimaryField != nil {
					builder.WriteQuoted(s.PrioritizedPrimaryField.DBName)
					builder.WriteByte(' ')
				} else {
					builder.WriteString("(SELECT NULL FROM ")
					builder.WriteString(d.DummyTableName())
					builder.WriteString(")")
				}
			}
		}

		if offset := limit.Offset; offset > 0 {
			builder.WriteString(" OFFSET ")
			builder.WriteString(strconv.Itoa(offset))
			builder.WriteString(" ROWS")
		}

		v := 0
		if limit.Limit != nil {
			v = *limit.Limit
		}
		if v > 0 {
			builder.WriteString(" FETCH NEXT ")
			builder.WriteString(strconv.Itoa(v))
			builder.WriteString(" ROWS ONLY")
		}
	}
}

// Oracle11 Limit
func (d Dialector) RewriteLimit11(c clause.Clause, builder clause.Builder) {
	limit, ok := c.Expression.(clause.Limit)
	if !ok {
		return
	}
	offsetRows := limit.Offset
	hasOffset := offsetRows > 0
	limitRows, hasLimit := d.getLimitRows(limit)
	if !hasOffset && !hasLimit {
		return
	}

	var stmt *gorm.Statement
	if stmt, ok = builder.(*gorm.Statement); !ok {
		return
	}

	if hasLimit && hasOffset {
		subQuerySQL := fmt.Sprintf(
			"SELECT * FROM (SELECT T.*, ROW_NUMBER() OVER (ORDER BY %s) AS %s FROM (%s) T) WHERE %s BETWEEN %d AND %d",
			d.getOrderByColumns(stmt),
			RowNumberAliasForOracle11,
			strings.TrimSpace(stmt.SQL.String()),
			RowNumberAliasForOracle11,
			offsetRows+1,
			offsetRows+limitRows,
		)

		stmt.SQL.Reset()
		stmt.SQL.WriteString(subQuerySQL)
	} else if hasLimit {
		// 只有 Limit 的情况
		subQuerySQL := fmt.Sprintf(
			"SELECT * FROM (%s) WHERE ROWNUM <= %d",
			strings.TrimSpace(stmt.SQL.String()),
			limitRows,
		)
		// d.rewriteRownumStmt(stmt, builder, " <= ", limitRows)

		stmt.SQL.Reset()
		stmt.SQL.WriteString(subQuerySQL)
	} else {
		// 只有 Offset 的情况
		// 偏移后取剩余所有记录
		subQuerySQL := fmt.Sprintf(
			"SELECT * FROM (SELECT T.*, ROW_NUMBER() OVER (ORDER BY %s) AS %s FROM (%s) T) WHERE %s > %d",
			d.getOrderByColumns(stmt),
			RowNumberAliasForOracle11,
			strings.TrimSpace(stmt.SQL.String()),
			RowNumberAliasForOracle11,
			offsetRows+1,
		)

		stmt.SQL.Reset()
		stmt.SQL.WriteString(subQuerySQL)

		// d.rewriteRownumStmt(stmt, builder, " > ", offsetRows)
	}
}

func (d Dialector) getOrderByColumns(stmt *gorm.Statement) string {
	if orderByClause, ok := stmt.Clauses["ORDER BY"]; ok {
		var orderBy clause.OrderBy
		if orderBy, ok = orderByClause.Expression.(clause.OrderBy); ok && len(orderBy.Columns) > 0 {
			orderByBuilder := strings.Builder{}
			for i, column := range orderBy.Columns {
				if i > 0 {
					orderByBuilder.WriteString(", ")
				}
				orderByBuilder.WriteString(column.Column.Name)
				if column.Desc {
					orderByBuilder.WriteString(" DESC")
				}
			}
			return orderByBuilder.String()
		}
	}
	return "NULL"
}

func (d Dialector) getLimitRows(limit clause.Limit) (limitRows int, hasLimit bool) {
	if l := limit.Limit; l != nil {
		limitRows = *l
		hasLimit = limitRows > 0
	}
	return
}

func (d Dialector) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{SQL: "VALUES (DEFAULT)"}
}

func (d Dialector) Migrator(db *gorm.DB) gorm.Migrator {
	return Migrator{
		Migrator: migrator.Migrator{
			Config: migrator.Config{
				DB:                          db,
				Dialector:                   d,
				CreateIndexAfterCreateTable: true,
			},
		},
	}
}

func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
	writer.WriteString(":")
	writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	if str != "" && IsReservedWord(str) {
		writer.WriteByte('"')
		writer.WriteString(str)
		writer.WriteByte('"')
	} else {

		writer.WriteString(str)
	}
}

var numericPlaceholder = regexp.MustCompile(`:(\d+)`)

func (d Dialector) Explain(sql string, vars ...interface{}) string {
	return logger.ExplainSQL(sql, numericPlaceholder, `'`, funk.Map(vars, func(v interface{}) interface{} {
		switch v := v.(type) {
		case bool:
			if v {
				return 1
			}
			return 0
		default:
			return v
		}
	}).([]interface{})...)
}

func (d Dialector) DataTypeOf(field *schema.Field) string {
	delete(field.TagSettings, "RESTRICT")

	var sqlType string

	switch field.DataType {
	case schema.Bool, schema.Int, schema.Uint, schema.Float:
		sqlType = "INTEGER"

		switch {
		case field.DataType == schema.Float:
			sqlType = "FLOAT"
		case field.Size <= 8:
			sqlType = "SMALLINT"
		}

		if val, ok := field.TagSettings["AUTOINCREMENT"]; ok && utils.CheckTruth(val) {
			sqlType += " GENERATED BY DEFAULT AS IDENTITY"
		}
	case schema.String, "VARCHAR2":
		size := field.Size
		defaultSize := d.DefaultStringSize

		if size == 0 {
			if defaultSize > 0 {
				size = int(defaultSize)
			} else {
				hasIndex := field.TagSettings["INDEX"] != "" || field.TagSettings["UNIQUE"] != ""
				// TEXT, GEOMETRY or JSON column can't have a default value
				if field.PrimaryKey || field.HasDefaultValue || hasIndex {
					size = 191 // utf8mb4
				}
			}
		}

		if size >= 2000 {
			sqlType = "CLOB"
		} else {
			sqlType = fmt.Sprintf("VARCHAR2(%d)", size)
		}

	case schema.Time:
		sqlType = "TIMESTAMP WITH TIME ZONE"

	case schema.Bytes:
		sqlType = "BLOB"
	default:
		sqlType = string(field.DataType)

		if strings.EqualFold(sqlType, "text") {
			sqlType = "CLOB"
		}

		if sqlType == "" {
			panic(fmt.Sprintf("invalid sql type %s (%s) for oracle", field.FieldType.Name(), field.FieldType.String()))
		}

	}

	return sqlType
}

func (d Dialector) SavePoint(tx *gorm.DB, name string) error {
	tx.Exec("SAVEPOINT " + name)
	return tx.Error
}

func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
	tx.Exec("ROLLBACK TO SAVEPOINT " + name)
	return tx.Error
}
