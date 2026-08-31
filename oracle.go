package oracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm/utils"

	// 生产链路仅支持 go-ora 驱动（驱动名 "oracle"）；godror 为已放弃的路线图项
	// _ "github.com/godror/godror"
	go_ora "github.com/sijms/go-ora/v2"
	"github.com/sijms/go-ora/v2/network"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"

	"github.com/charlienet/go-oracle/clauses"
	"github.com/charlienet/go-oracle/driver_adapter"
	oracleUtils "github.com/charlienet/go-oracle/utils"
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

// extendedStringLimit 返回 VARCHAR2 列的大小上限（字节）：
// 12c+ 且 Initialize 探测到 MAX_STRING_SIZE=EXTENDED → 32767；
// 其余情况（11g、未探测 Unknown、探测到 STANDARD）→ 4000，超过需用 CLOB。
func (d Dialector) extendedStringLimit() int {
	if d.Config != nil && supportsExtendedString(d.DBVer) && d.MaxStringSize == MaxStringSizeExtended {
		return 32767
	}
	return 4000
}

// supportsVector 是否支持 VECTOR 类型（23ai 引入，用于 AI Vector Search）
func supportsVector(dbVer string) bool { return oracleMajor(dbVer) >= OracleVersion23 }

// supportsMergeReturning 是否支持 MERGE 语句的 RETURNING 子句。
// 实测结论（Oracle 12.2.0.1, JEMPDB 容器）：MERGE 语句的 RETURNING 在任一
// 分支位置（WHEN MATCHED THEN UPDATE SET 之后 / WHEN NOT MATCHED THEN INSERT
// 之后）均报 ORA-00933: SQL command not properly ended；对照组 UPDATE/INSERT
// 的 RETURNING INTO 绑定正常（UPDATE...RETURNING 实测 err=nil）。
// 即 Oracle 的 MERGE 语句不支持 RETURNING 子句（语法限制，与版本无关），
// 故本函数恒返回 false——MERGE 分支不输出 RETURNING，避免 12c+ 带默认值
// 字段的 OnConflict 写入 ORA-00933。保留为显式开关：若未来确认某 Oracle
// 版本支持，可改为 oracleMajor(dbVer) >= X 并复核构建位置。
func supportsMergeReturning(dbVer string) bool { return false }

// isOracle11g 判断当前数据库是否低于 12c（11g 及以下不支持 IDENTITY 列）。
// 保留该函数以兼容既有调用，内部委托 supportsIdentity 取反。
func isOracle11g(dbVer string) bool {
	return !supportsIdentity(dbVer)
}

// MaxStringSize 表示数据库 MAX_STRING_SIZE 参数的三态探测结果
type MaxStringSize int

const (
	MaxStringSizeUnknown  MaxStringSize = iota // 未探测/探测失败（含 11g），保守按 STANDARD 处理
	MaxStringSizeStandard                      // VARCHAR2 上限 4000 字节
	MaxStringSizeExtended                      // VARCHAR2 上限 32767 字节
)

type Config struct {
	DriverName        string
	DSN               string
	Conn              gorm.ConnPool //*sql.DB
	DefaultStringSize uint
	DBName            string
	DBVer             string
	MaxStringSize     MaxStringSize // MAX_STRING_SIZE 探测结果（三态：Unknown/Standard/Extended），由 Initialize 探测数据库参数后填充；未探测/探测失败（含 11g）时为零值 Unknown，按 STANDARD 保守处理
	// DriverType 所请求的驱动类型。当前仅支持 go-ora：生产链路固定使用
	// go-ora 驱动，设置任何值（含 DriverGodror）均不改变实际驱动选择；
	// godror 支持未接线（Initialize 的驱动路径不读取该字段，仅 GetAdapter
	// 在零值 "" 时回退为 DriverGoOra）。保持零值即可；显式设置为非 "go-ora"
	// 的值时，Initialize 会打印一条告警提示配置无效。
	DriverType           driver_adapter.DriverType
	SkipQuoteIdentifiers bool // 新增：是否跳过标识符引用
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

	// d.DriverName = "godror" // 已停用：godror 未接线，当前仅支持 go-ora（默认驱动名 "oracle"）

	// DriverType 为预留字段：当前仅支持 go-ora，生产链路固定使用 go-ora 驱动；
	// 设置其他值（如 driver_adapter.DriverGodror）不改变实际驱动选择，仅告警提示配置无效
	if d.DriverType != "" && d.DriverType != driver_adapter.DriverGoOra {
		db.Logger.Warn(context.Background(), "oracle: Config.DriverType 当前未接线，设置 %q 无效，实际仍使用 go-ora 驱动（godror 支持未上线）", d.DriverType)
	}

	if d.DriverName == "" {
		d.DriverName = "oracle"
	}

	// godror.Batch // 已停用：godror 为路线图项，未接线

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

	// 探测 MAX_STRING_SIZE（仅 12c+ 有意义；11g 无此参数）
	if oracleMajor(d.DBVer) >= OracleVersion12 && d.Config != nil {
		var maxStr string
		// 必须用 database_properties：PDB 中 v$parameter 不返回 CDB 级参数 MAX_STRING_SIZE
		//（ISPDB_MODIFIABLE=FALSE），database_properties 在 PDB 可查且返回该 PDB 有效值
		if err := db.ConnPool.QueryRowContext(context.Background(),
			"SELECT property_value FROM database_properties WHERE property_name = 'MAX_STRING_SIZE'").Scan(&maxStr); err != nil {
			db.Logger.Warn(context.Background(), "oracle: MAX_STRING_SIZE 探测失败，按 STANDARD 保守处理: %v", err)
		} else {
			switch strings.ToUpper(strings.TrimSpace(maxStr)) {
			case "EXTENDED":
				d.MaxStringSize = MaxStringSizeExtended
			case "STANDARD":
				d.MaxStringSize = MaxStringSizeStandard
			}
		}
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

// ClauseBuilders 返回 Oracle 方言的 clause 构建器集合。
// 版本门控统一委托 supportsFetchOffset（单一来源，避免内联重复判定）：
//   - 版本解析失败（空/不可解析，oracleMajor==0）时保守使用 11g 的 ROWNUM 方案，
//     避免在 11g 下使用 12c+ 的 FETCH NEXT 语法导致 ORA-00933；
//   - >= 12c 使用 OFFSET/FETCH 分页语法。
//
// 同时注册 "FOR" 子句构建器（gorm 的 clause.Locking.Name() 返回 "FOR"，
// 对应 queryClauses 中的 "FOR"）：输出 Oracle 行锁语法 FOR UPDATE / FOR SHARE
// 退化 / NOWAIT / SKIP LOCKED，其中 SKIP LOCKED 按 12c+ 版本门控。
func (d Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	builders := map[string]clause.ClauseBuilder{
		"LIMIT": d.RewriteLimit11,
	}
	if supportsFetchOffset(d.DBVer) {
		builders["LIMIT"] = d.RewriteLimit
	}
	builders["FOR"] = func(c clause.Clause, builder clause.Builder) {
		if locking, ok := c.Expression.(clause.Locking); ok {
			clauses.Locking{
				Strength: locking.Strength,
				Table:    locking.Table.Name,
				Options:  locking.Options,
				// 11g 防护：SKIP LOCKED 是 12c 引入的语法，11g 下静默忽略
				AllowSkipLocked: func() bool { return supportsFetchOffset(d.DBVer) },
			}.Build(builder)
		}
	}
	return builders
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

	// 保留字必须引用
	if str != "" && IsReservedWord(str) {
		_, _ = writer.WriteString(`"` + str + `"`)
		return
	}

	// 检查是否为混合大小写（同时包含大写和小写字母需要引用）
	hasUpper := false
	hasLower := false
	for _, r := range str {
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		}
		if r >= 'a' && r <= 'z' {
			hasLower = true
		}
		// 提前退出：同时发现大小写
		if hasUpper && hasLower {
			break
		}
	}

	// 混合大小写需要引用
	if hasUpper && hasLower {
		_, _ = writer.WriteString(`"` + str + `"`)
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

	// gorm 的 unixtime serializer（schema.UnixSecondSerializer）语义：
	// int64 字段（Unix 秒）经序列化后以 time.Time 存储/读取——Value() 返回
	// time.Time，Scan() 经 sql.NullTime 接收（NUMBER 列读回的 int64 无法转 time.Time，
	// 会报 unsupported Scan）。因此列型须为 TIMESTAMP 系列（与 schema.Time 一致），
	// 若按 int64 默认的 INTEGER/NUMBER 映射，写入会触发
	// ORA-00932（expected NUMBER got TIMESTAMP），读取也会失败。
	if serializer, ok := schema.GetSerializer(field.TagSettings["SERIALIZER"]); ok {
		if _, isUnixSecond := serializer.(schema.UnixSecondSerializer); isUnixSecond {
			return "TIMESTAMP WITH TIME ZONE"
		}
	}

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

		// 支持 32k VARCHAR2 需同时满足：版本允许（12c+）且数据库实际启用
		// MAX_STRING_SIZE=EXTENDED（Initialize 时从 database_properties 探测）。
		// 未探测/探测失败/STANDARD 一律按 4000 字节上限保守处理，超过用 CLOB，
		// 避免在真实 MAX_STRING_SIZE=STANDARD 的库上生成 VARCHAR2(>4000) 导致
		// ORA-00910（无效的列长度）。
		limit := d.extendedStringLimit()
		if size > limit {
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
	case "vector":
		// Oracle 23ai 引入 VECTOR 类型（AI Vector Search）
		// 低于 23ai 的版本不支持，返回空字符串
		if !supportsVector(d.DBVer) {
			return ""
		}
		size := field.Size
		if size <= 0 {
			size = 1536 // 默认向量维度
		}
		sqlType = fmt.Sprintf("VECTOR(%d)", size)
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

		// text/json/clob 统一映射为 CLOB：
		// Oracle 21c+ 虽有原生 JSON 类型，但 go-ora（纯 Go）对 JSON 列仅按
		// LOB/文本传输、不做 OSON 解析；
		// 统一用 CLOB 存 JSON 文本对 11g~21c 各版本均兼容
		// （12c~19c 的 JSON 本就是 JSON 函数 + CLOB/VARCHAR2 存储）。
		if strings.EqualFold(sqlType, "text") || strings.EqualFold(sqlType, "json") || strings.EqualFold(sqlType, "clob") {
			sqlType = "CLOB"
		}

		// blob 类型同样规范化为大写
		if strings.EqualFold(sqlType, "blob") {
			sqlType = "BLOB"
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

// GetAdapter 返回驱动适配器。
// 注意：Config.DriverType 为预留字段，当前未接线——生产链路固定使用
// go-ora 驱动（Initialize 的驱动路径不读取该字段）；godror 适配器
// （driver_adapter/godror.go，受 build tag godror 约束）为预留实现，
// 未接入 Initialize。此方法仅保证默认返回 go-ora 适配器，供既有调用兼容。
func (d Dialector) GetAdapter() driver_adapter.Adapter {
	if d.DriverType == "" {
		d.DriverType = driver_adapter.DriverGoOra
	}
	return driver_adapter.Get(d.DriverType)
}

// oraErrorCodeRegex 匹配错误文本中的 ORA- 错误码（5 位数字，如 "ORA-00001"）
var oraErrorCodeRegex = regexp.MustCompile(`ORA-(\d{5})`)

// oraErrorCode 从错误中提取 Oracle 错误码。
//   - 优先结构化提取：go-ora 的错误为 *network.OracleError 结构体，
//     直接读取 ErrCode 字段（如 ORA-01400 的 ErrCode 为 1400），不依赖文本格式；
//   - 兜底用正则从错误文本提取 "ORA-xxxxx"（兼容 errors.New 模拟的纯文本错误
//     及第三方包装）；
//   - 提取不到返回 0（非 Oracle 错误）。
func oraErrorCode(err error) int {
	if err == nil {
		return 0
	}
	var oraErr *network.OracleError
	if errors.As(err, &oraErr) {
		return oraErr.ErrCode
	}
	if m := oraErrorCodeRegex.FindStringSubmatch(err.Error()); m != nil {
		if code, convErr := strconv.Atoi(m[1]); convErr == nil {
			return code
		}
	}
	return 0
}

// Translate 将 Oracle 错误映射为 GORM 标准错误。
// 优先基于 go-ora 结构化错误码（network.OracleError.ErrCode）映射；
// 无法结构化提取时回退到错误文本匹配（兼容纯文本/自定义包装错误）。
// 其余无对应 gorm.Err* 的 ORA 错误（如 ORA-00060 死锁、ORA-01722 类型转换
// 失败、ORA-12899 值过大、ORA-01438 值超出精度等）保持原样返回，不做包裹。
func (d Dialector) Translate(err error) error {
	if err == nil {
		return nil
	}

	switch oraErrorCode(err) {
	case 1: // ORA-00001: 唯一约束违反
		return gorm.ErrDuplicatedKey
	case 1403: // ORA-01403: 未找到数据
		return gorm.ErrRecordNotFound
	case 942: // ORA-00942: 表或视图不存在
		return gorm.ErrInvalidData
	case 1400: // ORA-01400: 无法插入 NULL（非空约束违反）
		// 注：gorm v1.31.2 未定义 ErrNotNullViolated（GORM 官方错误集无此错误），
		// 保持既有映射 gorm.ErrInvalidData，与 gorm 错误集保持一致
		return gorm.ErrInvalidData
	case 2291, 2292: // ORA-02291 / ORA-02292: 外键约束违反
		return gorm.ErrForeignKeyViolated
	case 2290: // ORA-02290: CHECK 约束违反
		return gorm.ErrCheckConstraintViolated
	}

	// 文本兜底：错误码结构化提取不到时，兼容纯文本错误（如 errors.New 模拟、
	// 非 go-ora 驱动的 ORA 文本）。保持与结构化映射一致的码表。
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "ORA-00001"):
		return gorm.ErrDuplicatedKey
	case strings.Contains(errStr, "ORA-01403"):
		return gorm.ErrRecordNotFound
	case strings.Contains(errStr, "ORA-00942"):
		return gorm.ErrInvalidData
	case strings.Contains(errStr, "ORA-01400"):
		return gorm.ErrInvalidData
	case strings.Contains(errStr, "ORA-02291") || strings.Contains(errStr, "ORA-02292"):
		return gorm.ErrForeignKeyViolated
	case strings.Contains(errStr, "ORA-02290"):
		return gorm.ErrCheckConstraintViolated
	}

	// 其他错误原样返回
	return err
}

// GetDBConn 返回底层数据库连接
// P1-7: 实现此方法以支持通过 GORM API 获取底层 *sql.DB
func (d *Dialector) GetDBConn() (*sql.DB, error) {
	if d.Conn != nil {
		if db, ok := d.Conn.(*sql.DB); ok {
			return db, nil
		}
	}
	return nil, fmt.Errorf("connection pool is not *sql.DB")
}

// GetOracleDriver 返回底层 go-ora 驱动
// P2-5: 解决 db.Driver() 返回 *enumNormalizeDriver 导致用户类型断言失败的问题
func GetOracleDriver(db *gorm.DB) (*go_ora.OracleDriver, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}

	dialector, ok := db.Dialector.(*Dialector)
	if !ok {
		return nil, fmt.Errorf("dialector is not oracle.Dialector")
	}

	// 从 *sql.DB 获取底层 driver
	sqlDB, err := dialector.GetDBConn()
	if err != nil {
		return nil, fmt.Errorf("cannot get *sql.DB: %w", err)
	}

	// sql.DB.Driver() 返回 driver.Driver 接口
	// 在 go-ora 路径下，这是 *enumNormalizeDriver
	driver := sqlDB.Driver()
	if wrapper, ok := driver.(*enumNormalizeDriver); ok {
		return wrapper.inner, nil
	}

	return nil, fmt.Errorf("cannot get underlying Oracle driver")
}
