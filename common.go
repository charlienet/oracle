package oracle

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	go_ora "github.com/sijms/go-ora/v2"
)

var (
	dateRegex      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timestampRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
)

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
	default:
		return 0
	}
}

// outParam 构造 RETURNING INTO 输出参数。统一使用 go_ora.Out（可携带 Size），
// 避免散落的 sql.Out 用法导致字符串输出参数 size=0。
func outParam(field *schema.Field) go_ora.Out {
	return go_ora.Out{Dest: reflect.New(field.FieldType).Interface(), Size: outParamSize(field)}
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
		if strings.EqualFold(field.DBName, "deleted_at") || field.Name == "DeletedAt" {
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
