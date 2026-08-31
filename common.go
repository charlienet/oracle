package oracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	go_ora "github.com/sijms/go-ora/v2"
)

var (
	dateRegex      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timestampRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
)

// execFinisher 在写入回调结束时由 defer 调用：自管事务时按 db.Error 决定
// Rollback/Commit 并经 db.AddError 上抛结果；非自管事务（外层负责）时 no-op。
type execFinisher func(db *gorm.DB)

// noopFinisher 非自管事务场景的空收尾：事务的提交/回滚由外层（gorm 系统回调
// 或调用方）负责，此处不做任何事。
func noopFinisher(db *gorm.DB) {}

// ensureWriteTx 返回可直接 ExecContext 的执行池。
// 仅当「当前不在任何事务中且池支持开事务」时包一层自管事务以保持批量原子性；
// 否则原样返回当前池。
//
// 背景（C-1 缺陷）：PrepareStmt 模式下 stmt.ConnPool 为 *gorm.PreparedStmtDB，
// 旧实现断言 *sql.Tx / Begin() (*sql.Tx, error) 均落空，导致所有写操作报
// "unsupported connection pool type"。本函数按池的具体类型分派：
//
//  1. *sql.Tx / *gorm.PreparedStmtTX：已在事务中，原样返回 + no-op 收尾；
//  2. *sql.DB：BeginTx 自管事务，收尾时按 db.Error 决定 Rollback/Commit；
//  3. *gorm.PreparedStmtDB：经其 BeginTx 得事务池（*gorm.PreparedStmtTX），
//     断言 gorm.TxCommitter 后按 2 处理；断言失败则原样返回 + no-op（防御，
//     理论不可达）；
//  4. 其他未知池：原样返回 + no-op，并经 logger.Warn 提示。
func ensureWriteTx(ctx context.Context, pool gorm.ConnPool, lg logger.Interface) (gorm.ConnPool, execFinisher, error) {
	switch p := pool.(type) {
	case *sql.Tx, *gorm.PreparedStmtTX:
		// 已在事务中：执行池原样可用，提交/回滚由外层负责
		return pool, noopFinisher, nil
	case *sql.DB:
		tx, err := p.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		return tx, func(db *gorm.DB) {
			// 回调执行失败（db.Error 非 nil）则回滚，否则提交；结果统一上抛
			if db.Error != nil {
				_ = db.AddError(tx.Rollback())
			} else {
				_ = db.AddError(tx.Commit())
			}
		}, nil
	case *gorm.PreparedStmtDB:
		txPool, err := p.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		committer, ok := txPool.(gorm.TxCommitter)
		if !ok {
			// 防御分支（理论不可达）：无法提交的事务池不如不开事务，
			// 尝试回滚已开启的事务后退化为直接在原池执行并告警。
			if lg != nil {
				lg.Warn(ctx, "oracle: PreparedStmtDB.BeginTx 返回了未实现 gorm.TxCommitter 的连接池: %T，尝试回滚后跳过事务包裹", txPool)
			}
			type rollbacker interface {
				Rollback() error
			}
			if rb, ok := txPool.(rollbacker); ok {
				if rbErr := rb.Rollback(); rbErr != nil {
					return nil, nil, fmt.Errorf("oracle: PreparedStmtDB.BeginTx 返回了未实现 gorm.TxCommitter 的连接池 %T，且回滚失败: %w", txPool, rbErr)
				}
			}
			return pool, noopFinisher, nil
		}
		return txPool, func(db *gorm.DB) {
			if db.Error != nil {
				_ = db.AddError(committer.Rollback())
			} else {
				_ = db.AddError(committer.Commit())
			}
		}, nil
	default:
		// 未知池类型：不强断言、不阻断写入（连接池自身通常具备语句级原子性），
		// 但失去批量原子性保证，需告警提示
		if lg != nil {
			lg.Warn(ctx, "oracle: 未识别的连接池类型 %T，写操作将跳过事务包裹（无批量原子性保证）", pool)
		}
		return pool, noopFinisher, nil
	}
}

// outParamSize 计算 RETURNING INTO 输出参数所需的缓冲区大小（字符数/字节数）。
// go-ora 对变长类型（字符串/RAW 等）输出参数必须指定 Size：
// 若 Size 为 0，驱动解析服务器返回值时会错位，导致 "more than one row affected
// with return clause" 或 "driver: bad connection"（见 go-ora issue #329/#703）。
// 数字/时间等定长类型不需要 Size。
// 注意：CLOB 字段的 RETURNING INTO 在 go-ora 下仍以 VARCHAR2 缓冲区处理，
// 超过 4000 字节的内容可能被截断，属已知限制。
func outParamSize(field *schema.Field) int {
	switch field.DataType {
	case schema.String, schema.Bytes:
		if field.Size > 0 {
			return field.Size
		}
		return 4000 // Oracle 11g VARCHAR2 上限
	case "text", "json":
		// CLOB/JSON 类型在 go-ora 的 RETURNING INTO 中仍以 VARCHAR2 缓冲区处理，
		// 使用与变长文本出参相同的缓冲上限。
		return 4000
	default:
		return 0
	}
}

// outParam 构造 RETURNING INTO 输出参数。统一使用 go_ora.Out（可携带 Size），
// 避免散落的 sql.Out 用法导致字符串输出参数 size=0。
//
// 对于命名基本类型（Go 枚举，如 type MerchantStatus int），Dest 使用对应裸基本
// 类型的指针（如 *int64），因为 go-ora 的 setDataType 对命名类型使用类型相等
// 比较会报 "unsupported go type"。回填时通过 GORM 的 field.Set 走 reflect
// ConvertibleTo 路径自动将裸值转换回命名类型，无需适配 create/update 代码。
func outParam(field *schema.Field) go_ora.Out {
	ft := field.FieldType
	// 解指针后取底层类型：*int64 → int64, *testStatus → testStatus
	if ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	// 对基本类型（裸或命名）统一使用裸基本类型指针：
	// int64 → *int64, testStatus(int) → *int64, string → *string
	// 非基本类型（struct 等）保持原样。
	if isBareKind(ft.Kind()) {
		ft = bareType(ft)
	}
	return go_ora.Out{Dest: reflect.New(ft).Interface(), Size: outParamSize(field)}
}

// isBareKind 判断 reflect.Kind 是否为基本类型（数值/布尔/字符串）。
func isBareKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Bool, reflect.String:
		return true
	}
	return false
}

// bareType 将命名基本类型的 reflect.Type 转换为对应裸类型的 reflect.Type。
// 例如 MerchantStatus（underlying int）→ int64，MerchantName（underlying string）→ string，
// MerchantBool（underlying bool）→ bool。整数统一使用 int64，浮点统一使用 float64，
// 与 database/sql DefaultParameterConverter 的转换目标一致。
func bareType(t reflect.Type) reflect.Type {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.TypeFor[int64]()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.TypeFor[uint64]()
	case reflect.Float32, reflect.Float64:
		return reflect.TypeFor[float64]()
	case reflect.Bool:
		return reflect.TypeFor[bool]()
	case reflect.String:
		return reflect.TypeFor[string]()
	default:
		return t
	}
}

// outDest 读取 RETURNING INTO 输出参数的目标指针
func outDest(vars []any, pos int) any {
	return vars[pos].(go_ora.Out).Dest
}

// convertValue 将 Go 值转换为 Oracle 兼容格式
func convertValue(value any, field *schema.Field) any {
	if value == nil {
		return value
	}

	switch v := value.(type) {
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		return v
	case driver.Valuer:
		// 调用 Value() 方法解包
		val, err := v.Value()
		if err != nil {
			return value // 如果出错，返回原始值
		}
		return val
	case time.Time:
		return v
	default:
		return value
	}
}

// convertFromOracleToField 将 Oracle 返回值转换为 Go 类型
// 处理 Oracle 特有的类型转换，包括 LOB 类型（CLOB/BLOB）
func convertFromOracleToField(value any, field *schema.Field) any {
	if value == nil {
		return value
	}

	switch v := value.(type) {
	case sql.NullTime:
		if v.Valid {
			return v.Time
		}
		return nil
	case sql.NullInt64:
		if v.Valid {
			return v.Int64
		}
		return nil
	case sql.NullFloat64:
		if v.Valid {
			return v.Float64
		}
		return nil
	case sql.NullBool:
		if v.Valid {
			return v.Bool
		}
		return nil
	case sql.NullString:
		if v.Valid {
			return v.String
		}
		return nil
	case go_ora.Clob:
		// go-ora 对 CLOB 列返回 go_ora.Clob 类型，需要解包为 string
		// 这样 GORM 的 serializer（如 json/gob）才能正确处理
		if v.Valid {
			return v.String
		}
		return nil
	case go_ora.Blob:
		// go-ora 对 BLOB 列返回 go_ora.Blob 类型，需要解包为 []byte
		// 这样 GORM 的 serializer（如 gob）才能正确处理
		if v.Valid {
			return v.Data
		}
		return nil
	case *go_ora.Clob:
		// 处理指针类型的 Clob
		if v != nil && v.Valid {
			return v.String
		}
		return nil
	case *go_ora.Blob:
		// 处理指针类型的 Blob
		if v != nil && v.Valid {
			return v.Data
		}
		return nil
	case string:
		// 如果字段期望 []byte 类型，尝试将 string 转换为 []byte
		// 这支持了多种场景：
		// 1. 字段类型是 schema.Bytes（如 BLOB 列）
		// 2. 字段的实际 Go 类型是 []byte（如使用 serializer:gob 的字段）
		// 3. go-ora 将 BLOB 数据作为十六进制编码字符串返回
		if field != nil {
			// 检查字段类型是否是 Bytes
			isBytesField := field.DataType == schema.Bytes

			// 检查字段的实际 Go 类型是否是 []byte
			if !isBytesField && field.FieldType != nil {
				isBytesField = field.FieldType.Kind() == reflect.Slice &&
					field.FieldType.Elem().Kind() == reflect.Uint8
			}

			if isBytesField {
				// 优先尝试 hex 解码（go-ora 可能返回十六进制编码的字符串）
				if decoded, err := hex.DecodeString(v); err == nil {
					return decoded
				}
				// 如果 hex 解码失败，直接转换为 []byte
				// 这适用于直接存储的二进制数据
				return []byte(v)
			}
		}
		return value
	default:
		return value
	}
}

// validateCreateData 验证创建数据
func validateCreateData(data any) error {
	if data == nil {
		return fmt.Errorf("create data cannot be nil")
	}

	rv := reflect.ValueOf(data)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return fmt.Errorf("create data pointer cannot be nil")
		}
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Slice {
		if rv.Len() == 0 {
			return fmt.Errorf("create data slice cannot be empty")
		}
	}

	return nil
}

// checkMissingWhereConditions 检查 WHERE 条件是否缺失
func checkMissingWhereConditions(conditions []clause.Expression, schema *schema.Schema) bool {
	if len(conditions) == 0 {
		return true
	}
	count := 0
	for _, condition := range conditions {
		// 检查是否是软删除条件 (deleted_at IS NULL)
		if isSoftDeleteCondition(condition, schema) {
			count++
		}
	}

	// 如果只有软删除条件或没有其他条件，则认为缺少WHERE条件
	return count >= len(conditions)
}

// isSoftDeleteCondition 检查条件是否为软删除条件（deleted_at IS NULL）
func isSoftDeleteCondition(condition clause.Expression, sch *schema.Schema) bool {
	// 查找是否有 deleted_at 字段
	var softDeleteField *schema.Field
	for _, field := range sch.Fields {
		// 优先检查类型（最精确）
		if reflect.TypeOf(field.FieldType) == reflect.TypeFor[gorm.DeletedAt]() {
			softDeleteField = field
			break
		}
		// 兼容旧逻辑：按字段名判定，但要求类型必须是 time 相关
		if (field.Name == "DeletedAt" || strings.EqualFold(field.DBName, "deleted_at")) &&
			(field.DataType == schema.Time || field.GORMDataType == schema.Time) {
			softDeleteField = field
			break
		}
	}

	if softDeleteField == nil {
		return false
	}

	// GORM 的软删除条件在 WHERE 中表现为对 deleted_at 列的相等/包含判断（值为空，构建为 IS NULL）
	switch cond := condition.(type) {
	case clause.Eq:
		return strings.EqualFold(columnNameOf(cond.Column), softDeleteField.DBName)
	case clause.IN:
		return strings.EqualFold(columnNameOf(cond.Column), softDeleteField.DBName)
	case clause.Neq:
		return strings.EqualFold(columnNameOf(cond.Column), softDeleteField.DBName)
	default:
		return false
	}
}

// columnNameOf 从 clause 表达式的 Column 字段中提取列名
func columnNameOf(col any) string {
	switch c := col.(type) {
	case clause.Column:
		return c.Name
	case string:
		return c
	}
	return ""
}

// addPrimaryKeyWhere 根据模型的主键值注入 WHERE 条件。
// GORM 默认的 Update/Delete 回调会在语句中注入主键条件，
// 但驱动自定义回调替换了默认实现，因此需要手动补齐。
// 返回注入的主键值数量（0 表示没有可用的主键值）。
func addPrimaryKeyWhere(stmt *gorm.Statement, sch *schema.Schema) int {
	if stmt == nil || sch == nil {
		return 0
	}

	_, queryValues := schema.GetIdentityFieldValuesMap(stmt.Context, stmt.ReflectValue, sch.PrimaryFields)
	column, values := schema.ToQueryValues(stmt.Table, sch.PrimaryFieldDBNames, queryValues)
	if len(values) > 0 {
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{clause.IN{Column: column, Values: values}}})
	}
	return len(values)
}

// escapeOracleString 转义 Oracle 字符串中的单引号
func escapeOracleString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// buildOracleDefault 智能转换默认值
// dbVer 用于版本感知的默认值处理：Oracle 11g 的 DEFAULT 子句不允许引用序列的
// NEXTVAL（ORA-00984，12c 才引入该能力），因此 11g 下 NEXTVAL 分支返回空字符串，
// 由调用方通过 BEFORE INSERT 触发器实现等价语义（见 migrator.createSequenceDefaultTrigger）。
func buildOracleDefault(dbVer string, defaultValue string, field *schema.Field) string {
	if defaultValue == "" {
		return ""
	}

	lowerVal := strings.ToLower(strings.TrimSpace(defaultValue))

	switch lowerVal {
	case "null":
		return "DEFAULT NULL"
	case "current_timestamp", "now()":
		return "DEFAULT CURRENT_TIMESTAMP"
	case "sysdate":
		return "DEFAULT SYSDATE"
	case "true":
		return "DEFAULT 1"
	case "false":
		return "DEFAULT 0"
	default:
		// 检查是否为序列
		if strings.Contains(strings.ToUpper(defaultValue), ".NEXTVAL") {
			// 12c+ 原生支持 DEFAULT <seq>.NEXTVAL，直接生成 DEFAULT 子句
			if supportsIdentity(dbVer) {
				// 去掉可能的包裹括号（GORM 对含括号的默认值保持原文，
				// 如 "(SEQ_MY.NEXTVAL)"），生成标准的 DEFAULT <seq>.NEXTVAL
				seqExpr := strings.TrimSpace(defaultValue)
				seqExpr = strings.TrimPrefix(seqExpr, "(")
				seqExpr = strings.TrimSuffix(seqExpr, ")")
				return fmt.Sprintf("DEFAULT %s", strings.TrimSpace(seqExpr))
			}
			// 11g 不支持 DEFAULT 子句引用序列（ORA-00984），
			// 返回空字符串，由建表流程创建 BEFORE INSERT 触发器实现等价语义
			return ""
		}

		// 检查日期格式 "2006-01-02"
		if dateRegex.MatchString(defaultValue) {
			return fmt.Sprintf("DEFAULT TO_DATE('%s', 'YYYY-MM-DD')", escapeOracleString(defaultValue))
		}

		// 检查时间戳格式 "2006-01-02 15:04:05"
		if timestampRegex.MatchString(defaultValue) {
			return fmt.Sprintf("DEFAULT TO_DATE('%s', 'YYYY-MM-DD HH24:MI:SS')", escapeOracleString(defaultValue))
		}

		// 普通字符串用单引号包围
		return fmt.Sprintf("DEFAULT '%s'", escapeOracleString(defaultValue))
	}
}
