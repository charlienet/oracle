package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
	"gorm.io/gorm/utils"

	_ "github.com/godror/godror"
	_ "github.com/sijms/go-ora/v2"
)

const (
	RowNumberAliasForOracle11 = "ROW_NUM"
)

func Hello() string {
	return "GORM Oracle Driver"
}

type Config struct {
	DriverName        string
	DSN               string
	Conn              *sql.DB
	DefaultStringSize uint
	DBVer             string
	DBName            string // 库名
}

type Dialector struct {
	*Config
}

func New(config Config) gorm.Dialector {
	return &Dialector{Config: &config}
}

func Open(dsn string) gorm.Dialector {
	return &Dialector{Config: &Config{DSN: dsn}}
}

func (d Dialector) DummyTableName() string { return "DUAL" }

func (d Dialector) Name() string { return "oracle" }

func (d Dialector) Initialize(db *gorm.DB) (err error) {
	db.NamingStrategy = Namer{
		NamingStrategy: db.NamingStrategy,
		DBName:         d.DBName,
	}
	d.DefaultStringSize = 1024

	// register callbacks
	callbackConfig := &callbacks.Config{}
	callbacks.RegisterDefaultCallbacks(db, callbackConfig)

	// d.DriverName = "godror"
	d.DriverName = "oracle"

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

	db.Logger.Info(context.Background(), "DBVer:%s", d.DBVer)

	if err = db.Callback().Create().Replace("gorm:create", Create); err != nil {
		return
	}

	for k, v := range d.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}

	return
}

func (d Dialector) ClauseBuilders() (clauseBuilders map[string]clause.ClauseBuilder) {
	clauseBuilders = make(map[string]clause.ClauseBuilder)
	if dbVer, _ := strconv.Atoi(strings.Split(d.DBVer, ".")[0]); dbVer > 11 {
		clauseBuilders["LIMIT"] = d.RewriteLimit
	} else {
		clauseBuilders["LIMIT"] = d.RewriteLimit11
	}

	return
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
		if limit := limit.Limit; *limit > 0 {
			builder.WriteString(" FETCH NEXT ")
			builder.WriteString(strconv.Itoa(*limit))
			builder.WriteString(" ROWS ONLY")
		}
	}
}

func (d Dialector) RewriteLimit11(c clause.Clause, builder clause.Builder) {
	println("rewrite limit oracle 11g")
	limit, ok := c.Expression.(clause.Limit)
	if !ok {
		return
	}

	offsetRows := limit.Offset
	hasOffset := offsetRows > 0
	limitRows, hasLimit := d.getLimitRows(limit)
	if (!hasOffset && !hasLimit) || limitRows == 1 {
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
		// only limit
		d.rewriteRownumStmt(stmt, builder, " <= ", limitRows)
	} else {
		// only offset
		d.rewriteRownumStmt(stmt, builder, " > ", offsetRows)
	}
}

func (d Dialector) rewriteRownumStmt(stmt *gorm.Statement, builder clause.Builder, operator string, rows int) {
	limitSql := strings.Builder{}
	if _, ok := stmt.Clauses["WHERE"]; !ok {
		limitSql.WriteString(" WHERE ")
	} else {
		limitSql.WriteString(" AND ")
	}
	limitSql.WriteString("ROWNUM")
	limitSql.WriteString(operator)
	limitSql.WriteString(strconv.Itoa(rows))

	if _, hasOrderBy := stmt.Clauses["ORDER BY"]; !hasOrderBy {
		_, _ = builder.WriteString(limitSql.String())
	} else {
		// "ORDER BY" before insert
		sqlTmp := strings.Builder{}
		sqlOld := stmt.SQL.String()
		orderIndex := strings.Index(sqlOld, "ORDER BY") - 1
		sqlTmp.WriteString(sqlOld[:orderIndex])
		sqlTmp.WriteString(limitSql.String())
		sqlTmp.WriteString(sqlOld[orderIndex:])
		stmt.SQL = sqlTmp
	}
}

func (d Dialector) getOrderByColumns(stmt *gorm.Statement) string {
	if orderByClause, ok := stmt.Clauses["ORDER BY"]; ok {
		var orderBy clause.OrderBy

		if orderBy, ok = orderByClause.Expression.(clause.OrderBy); ok && len(orderBy.Columns) > 0 {
			println("order columns:", orderBy.Columns)

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
	case schema.String:
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
		if field.NotNull || field.PrimaryKey {
			sqlType += " NOT NULL"
		}
	case schema.Bytes:
		sqlType = "BLOB"
	default:
		sqlType := string(field.DataType)

		if strings.EqualFold(sqlType, "text") {
			sqlType = "CLOB"
		}

		if sqlType == "" {
			panic(fmt.Sprintf("invalid sql type %s (%s) for oracle", field.FieldType.Name(), field.FieldType.String()))
		}

		notNull := field.TagSettings["NOT NULL"]
		unique := field.TagSettings["UNIQUE"]
		additionalType := fmt.Sprintf("%s %s", notNull, unique)
		if value, ok := field.TagSettings["DEFAULT"]; ok {
			additionalType = fmt.Sprintf("%s %s %s%s", "DEFAULT", value, additionalType, func() string {
				if value, ok := field.TagSettings["COMMENT"]; ok {
					return " COMMENT " + value
				}
				return ""
			}())
		}

		sqlType = fmt.Sprintf("%v %v", sqlType, additionalType)
		return sqlType
	}

	return sqlType
}

func (d Dialector) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{SQL: "VALUES (DEFAULT)"}
}

func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	writer.WriteString(str)
}

var numericPlaceholder = regexp.MustCompile(`:(\d+)`)

func (d Dialector) Explain(sql string, vars ...any) string {
	return logger.ExplainSQL(sql, numericPlaceholder, `'`, funk.Map(vars, func(v any) any {
		switch v := v.(type) {
		case bool:
			if v {
				return 1
			}
			return 0
		default:
			return v
		}
	}).([]any)...)
}

func (d Dialector) SavePoint(tx *gorm.DB, name string) error {
	tx.Exec("SAVEPOINT " + name)
	return tx.Error
}

func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
	tx.Exec("ROLLBACK TO SAVEPOINT " + name)
	return tx.Error
}
