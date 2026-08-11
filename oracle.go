package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm/utils"

	// _ "github.com/godror/godror"
	_ "github.com/sijms/go-ora/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"

	"github.com/charlienet/oracle/driver_adapter"
	oracleUtils "github.com/charlienet/oracle/utils"
)

const RowNumberAliasForOracle11 = "ROW_NUM"

// Oracle 版本主版本号常量（对应各版本引入的数据库特性）
const (
	OracleVersion10 = 10 // Oracle 10g
	OracleVersion11 = 11 // Oracle 11g（不含 IDENTITY 列、OFFSET/FETCH 分页）
	OracleVersion12 = 12 // Oracle 12c（引入 IDENTITY 列、OFFSET/FETCH 分页；12.1 起支持 Extended 32k VARCHAR2）
	OracleVersion18 = 18 // Oracle 18c（12.2 的再版）
	OracleVersion19 = 19 // Oracle 19c
	OracleVersion21 = 21 // Oracle 21c（引入原生 BOOLEAN 列类型）
	OracleVersion23 = 23 // Oracle 23ai（引入 VECTOR 类型）
)

// oracleMajor 返回数据库版本的主版本号；解析失败返回 0。
// 支持格式如 "11.2.0.4.0"、"19.0.0.0.0"、"23.0.0.0.0"。
func oracleMajor(dbVer string) int {
	major, _ := strconv.Atoi(strings.Split(dbVer, ".")[0])
	return major
}

// supportsIdentity 是否支持 IDENTITY 列（12c+ 支持 GENERATED ... AS IDENTITY；
// 11g 及以下需用序列 + BEFORE INSERT 触发器模拟自增）
func supportsIdentity(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion12 }

// supportsFetchOffset 是否支持 OFFSET/FETCH 分页语法（12c+ 引入；
// 11g 需改写为 ROWNUM 分页）
func supportsFetchOffset(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion12 }

// supportsNativeBoolean 是否支持原生 BOOLEAN 列类型（21c+ 引入；
// 更早版本需用 NUMBER(1) 模拟）
func supportsNativeBoolean(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion21 }

// supportsExtendedString 是否支持 Extended 32k VARCHAR2（12.2+ 默认开启；
// 12.1 需 MAX_STRING_SIZE=EXTENDED）。保守判定：主版本 >= 12 视为可能支持，
// 具体是否生效依赖数据库参数。
func supportsExtendedString(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion12 }

// supportsVector 是否支持 VECTOR 类型（23ai 引入，用于 AI Vector Search）
func supportsVector(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion23 }

// isOracle11g 判断当前数据库是否低于 12c（11g 及以下不支持 IDENTITY 列）。
// 保留该函数以兼容既有调用，内部委托 supportsIdentity 取反。
func isOracle11g(dbVer string) bool {
	return !supportsIdentity(dbVer)
}

type Config struct {
	DriverName           string
	DSN                  string
	Conn                 gorm.ConnPool //*sql.DB
	DefaultStringSize    uint
	DBName               string
	DBVer                string
	DriverType           driver_adapter.DriverType // 新增：驱动类型（go-ora 或 godror）
	SkipQuoteIdentifiers bool                      // 新增：是否跳过标识符引用
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

	// 注册 Update 回调
	if err = db.Callback().Update().Replace("gorm:update", Update); err != nil {
		return
	}

	// 注册 Delete 回调
	if err = db.Callback().Delete().Replace("gorm:delete", Delete); err != nil {
		return
	}

	// 注册 Query 回调
	if err = db.Callback().Query().Replace("gorm:query", Query); err != nil {
		return
	}

	maps.Copy(db.ClauseBuilders, d.ClauseBuilders())
	return
}

func (d Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	dbver, _ := strconv.Atoi(strings.Split(d.DBVer, ".")[0])
	// 版本解析失败（dbver==0）时保守使用 11g 的 ROWNUM 方案，
	// 避免在 11g 下使用 12c+ 的 FETCH NEXT 语法导致 ORA-00933
	if dbver == 0 || dbver < 12 {
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

func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {
	writer.WriteString(":")
	writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	if d.SkipQuoteIdentifiers {
		writer.WriteString(str)
		return
	}

	if str != "" && IsReservedWord(str) {
		writer.WriteByte('"')
		writer.WriteString(str)
		writer.WriteByte('"')
	} else {
		writer.WriteString(str)
	}
}

var numericPlaceholder = regexp.MustCompile(`:(\d+)`)

func (d Dialector) Explain(sql string, vars ...any) string {
	return logger.ExplainSQL(sql, numericPlaceholder, `'`, oracleUtils.MapInterface(vars, func(v any) any {
		switch v := v.(type) {
		case bool:
			if v {
				return 1
			}
			return 0
		default:
			return v
		}
	})...)
}

func (d Dialector) DataTypeOf(field *schema.Field) string {
	delete(field.TagSettings, "RESTRICT")

	var sqlType string

	switch field.DataType {
	case schema.Bool:
		// Oracle 21c+ 支持原生 BOOLEAN 列；更早版本用 NUMBER(1) 模拟
		if supportsNativeBoolean(d.DBVer) {
			sqlType = "BOOLEAN"
		} else {
			sqlType = "NUMBER(1)"
		}
	case schema.Int, schema.Uint:
		sqlType = "INTEGER"
		if field.Size <= 8 {
			sqlType = "SMALLINT"
		}
		// Oracle 12c+ 支持 IDENTITY 列；Oracle 11g 需要在迁移时创建序列 + 触发器
		if field.AutoIncrement && supportsIdentity(d.DBVer) {
			sqlType += " GENERATED BY DEFAULT AS IDENTITY"
		}
	case schema.Float:
		sqlType = "FLOAT"

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

		// Oracle 12c+（Extended）支持最长 32767 字节的 VARCHAR2（32k 特性）；
		// 11g 及未开启 Extended 的库超过 4000 必须用 CLOB。
		// 保守策略：
		//   - size 在 2000~4000 之间维持 CLOB 不变（保持历史行为）
		//   - size > 4000 且版本 >= 12 → VARCHAR2(size)（利用 32k 特性）
		//   - size > 4000 且 11g → CLOB（保持现状）
		if size > 4000 {
			if supportsExtendedString(d.DBVer) {
				sqlType = fmt.Sprintf("VARCHAR2(%d)", size)
			} else {
				sqlType = "CLOB"
			}
		} else if size >= 2000 {
			sqlType = "CLOB"
		} else {
			sqlType = fmt.Sprintf("VARCHAR2(%d)", size)
		}

	case schema.Time:
		if field.Precision > 0 {
			sqlType = fmt.Sprintf("TIMESTAMP(%d) WITH TIME ZONE", field.Precision)
		} else {
			sqlType = "TIMESTAMP WITH TIME ZONE"
		}

	case schema.Bytes:
		sqlType = "BLOB"
	default:
		sqlType = string(field.DataType)

		// text/json 统一映射为 CLOB：
		// Oracle 21c+ 虽有原生 JSON 类型，但 go-ora（纯 Go）对 JSON 列仅按
		// LOB/文本传输、不做 OSON 解析，且 godror 依赖 ODPI-C/Instant Client；
		// 统一用 CLOB 存 JSON 文本对 11g~21c 各版本与两个驱动都兼容
		// （12c~19c 的 JSON 本就是 JSON 函数 + CLOB/VARCHAR2 存储）。
		if strings.EqualFold(sqlType, "text") || strings.EqualFold(sqlType, "json") {
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

func (d Dialector) GetAdapter() driver_adapter.Adapter {
	if d.DriverType == "" {
		d.DriverType = driver_adapter.DriverGoOra
	}
	return driver_adapter.Get(d.DriverType)
}
