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
	go_ora "github.com/sijms/go-ora/v2"
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
	if d.DefaultStringSize == 0 {
		d.DefaultStringSize = 1024
	}

	// register callbacks
	//callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{WithReturning: true})
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{
		CreateClauses: []string{"INSERT", "VALUES", "ON CONFLICT", "RETURNING"},
		UpdateClauses: []string{"UPDATE", "SET", "WHERE", "RETURNING"},
		DeleteClauses: []string{"DELETE", "FROM", "WHERE", "RETURNING"},
	})

	// d.DriverName = "godror"
	if d.DriverName == "" {
		d.DriverName = "oracle"
	}

	// godror.Batch

	if d.Conn != nil {
		db.ConnPool = d.Conn
	} else if d.DriverName == "" || d.DriverName == "oracle" {
		// go-ora 驱动路径：包装驱动，在参数绑定前将底层为基本类型的
		// 自定义类型（Go 枚举）规范化为裸基本类型，避免命名类型在
		// go-ora 的 setDataType 中坠入 UDT/unsupported 分支报错
		goOraDriver := &enumNormalizeDriver{inner: go_ora.NewDriver()}
		connector, err := goOraDriver.OpenConnector(d.DSN)
		if err != nil {
			return err
		}
		db.ConnPool = sql.OpenDB(connector)
	} else {
		db.ConnPool, err = sql.Open(d.DriverName, d.DSN)
		if err != nil {
			return
		}
	}
	err = db.ConnPool.QueryRowContext(context.Background(), "select version from product_component_version where rownum = 1").Scan(&d.DBVer)
	if err != nil {
		// 版本探测失败不阻断连接：降级为空版本号，全链路按保守的 11g 行为运行
		//（ROWNUM 分页、NUMBER(1) 布尔、无 IDENTITY），保证 DryRun/离线/受限账号可用
		d.DBVer = ""
		db.Logger.Warn(context.Background(), "oracle: 获取数据库版本失败，将按保守的 11g 行为运行: %v", err)
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
				_, _ = builder.WriteString("ORDER BY ")
				if s != nil && s.PrioritizedPrimaryField != nil {
					builder.WriteQuoted(s.PrioritizedPrimaryField.DBName)
					_ = builder.WriteByte(' ')
				} else {
					_, _ = builder.WriteString("(SELECT NULL FROM ")
					_, _ = builder.WriteString(d.DummyTableName())
					_, _ = builder.WriteString(")")
				}
			}
		}

		if offset := limit.Offset; offset > 0 {
			_, _ = builder.WriteString(" OFFSET ")
			_, _ = builder.WriteString(strconv.Itoa(offset))
			_, _ = builder.WriteString(" ROWS")
		}

		v := 0
		if limit.Limit != nil {
			v = *limit.Limit
		}
		if v > 0 {
			_, _ = builder.WriteString(" FETCH NEXT ")
			_, _ = builder.WriteString(strconv.Itoa(v))
			_, _ = builder.WriteString(" ROWS ONLY")
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
		// 偏移后取剩余所有记录：跳过前 offsetRows 行（ROW_NUM > offsetRows），
		// 即返回第 offsetRows+1 行起的数据，与 limit+offset 分支的
		// BETWEEN offsetRows+1 AND offsetRows+limitRows 语义一致
		subQuerySQL := fmt.Sprintf(
			"SELECT * FROM (SELECT T.*, ROW_NUMBER() OVER (ORDER BY %s) AS %s FROM (%s) T) WHERE %s > %d",
			d.getOrderByColumns(stmt),
			RowNumberAliasForOracle11,
			strings.TrimSpace(stmt.SQL.String()),
			RowNumberAliasForOracle11,
			offsetRows,
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
	// 没有 ORDER BY 时使用主键列作为默认排序，避免分页结果不稳定
	if stmt.Schema != nil && stmt.Schema.PrioritizedPrimaryField != nil {
		return stmt.Schema.PrioritizedPrimaryField.DBName
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
	return clause.Expr{SQL: "DEFAULT"}
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
	_, _ = writer.WriteString(":")
	_, _ = writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	if d.SkipQuoteIdentifiers {
		_, _ = writer.WriteString(str)
		return
	}

	if str != "" && IsReservedWord(str) {
		_ = writer.WriteByte('"')
		_, _ = writer.WriteString(str)
		_ = writer.WriteByte('"')
	} else {
		_, _ = writer.WriteString(str)
	}
}

var numericPlaceholder = regexp.MustCompile(`:(\d+)`)
var savepointNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

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

		// 用户显式 type 标签（如 "varchar(1)"、"VARCHAR(20)"、"varchar2(64)"）大小写不敏感
		// 归一到 VARCHAR2(n)：保证 AutoMigrate 时 fullDataType 与 Oracle 数据字典
		//（VARCHAR2）一致，避免每次迁移都误判列类型差异而反复 ALTER。
		// 注意 nvarchar/nvarchar2 语义不同（国家字符集），保持原样不归一。
		lower := strings.ToLower(sqlType)
		if strings.HasPrefix(lower, "varchar") && !strings.HasPrefix(lower, "nvarchar") {
			size := 0
			if i := strings.IndexByte(lower, '('); i > 0 && strings.HasSuffix(lower, ")") {
				if n, err := strconv.Atoi(lower[i+1 : len(lower)-1]); err == nil {
					size = n
				}
			}
			if size <= 0 {
				size = int(d.DefaultStringSize)
				if size <= 0 {
					size = 1024
				}
			}
			sqlType = fmt.Sprintf("VARCHAR2(%d)", size)
		}

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
	if !savepointNameRegex.MatchString(name) {
		return fmt.Errorf("invalid savepoint name: %s", name)
	}
	tx.Exec("SAVEPOINT " + name)
	return tx.Error
}

func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
	if !savepointNameRegex.MatchString(name) {
		return fmt.Errorf("invalid savepoint name: %s", name)
	}
	tx.Exec("ROLLBACK TO SAVEPOINT " + name)
	return tx.Error
}

func (d Dialector) GetAdapter() driver_adapter.Adapter {
	if d.DriverType == "" {
		d.DriverType = driver_adapter.DriverGoOra
	}
	return driver_adapter.Get(d.DriverType)
}
