package oracle

import (
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"

	"github.com/charlienet/go-oracle/utils"
)

func Update(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil {
		return
	}
	schema := stmt.Schema
	if schema == nil {
		return
	}

	boundVars := make(map[string]int)
	hasDefaultValues := len(schema.FieldsWithDefaultDBValue) > 0

	if !stmt.Unscoped {
		for _, c := range schema.UpdateClauses {
			stmt.AddClause(c)
		}
	}

	// 注入主键 WHERE 条件（GORM 默认回调会做这一步）
	pkValues := addPrimaryKeyWhere(stmt, schema)

	// 多行更新时 Oracle 不支持单行 RETURNING INTO，只有单行更新才启用 RETURNING
	if pkValues != 1 {
		hasDefaultValues = false
	}

	// WHERE 安全检查
	where, hasWhere := stmt.Clauses["WHERE"].Expression.(clause.Where)
	if hasWhere {
		if checkMissingWhereConditions(where.Exprs, schema) {
			_ = db.AddError(fmt.Errorf("missing WHERE condition in UPDATE"))
			return
		}
	} else {
		// 没有 WHERE 子句，且模型主键没有可用的值
		_ = db.AddError(fmt.Errorf("missing WHERE condition in UPDATE"))
		return
	}

	if stmt.SQL.String() == "" {
		// 构建 UPDATE 语句
		stmt.AddClauseIfNotExists(clause.Update{Table: clause.Table{Name: stmt.Schema.Table}})

		// 构建 SET 子句
		// 从 stmt.Dest 获取待更新的数据
		reflectValue := reflect.ValueOf(stmt.Dest)
		if reflectValue.Kind() == reflect.Pointer {
			reflectValue = reflectValue.Elem()
		}

		// 解析 Select/Omit 约束
		selectColumns, restricted := stmt.SelectAndOmitColumns(false, true)

		// 构建 SET 表达式
		sets := make(clause.Set, 0)
		switch reflectValue.Kind() {
		case reflect.Struct:
			for _, field := range schema.Fields {
				if !field.PrimaryKey && field.Updatable {
					// 选中条件与 GORM ConvertToAssignments 完全一致：
					// (ok && v)：显式 Select 的列（含 Save 的 Selects=["*"] 全字段语义）；
					// !ok && !restricted：无 Select/Omit 约束；
					// !ok && restricted && AutoUpdateTime>0：受限下 AutoUpdateTime 字段仍处理
					v, ok := selectColumns[field.DBName]
					if (!ok || !v) && (ok || (restricted && (stmt.SkipHooks || field.AutoUpdateTime <= 0))) {
						continue
					}
					fieldValue, isZero := field.ValueOf(stmt.Context, reflectValue)
					if !stmt.SkipHooks && field.AutoUpdateTime > 0 {
						fieldValue = autoUpdateTimeValue(stmt, field)
						isZero = false
					}
					// GORM: (ok || !isZero) && field.Updatable（Updatable 已在外层判断）
					// 显式选中（含 Save 的 Selects=["*"]）时零值字段也进入 SET
					if ok || !isZero {
						convertedValue := convertValue(fieldValue, field)
						sets = append(sets, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: convertedValue})
					}
				}
			}
		case reflect.Map:
			// GORM map 语义：无主键过滤；无对应 field 的键按原列名处理
			for _, mapKey := range reflectValue.MapKeys() {
				key := mapKey.String()
				value := reflectValue.MapIndex(mapKey).Interface()
				// 对齐 GORM ConvertToAssignments：值为 *gorm.DB（子查询）时包装为
				// []interface{}{kv}，让 AddVar 生成带括号的 col=(SELECT ...)
				if _, ok := value.(*gorm.DB); ok {
					value = []any{value}
				}
				field := schema.LookUpField(key)
				if field == nil {
					// 大小写不敏感回退：用户可能用全小写 DBName 键（如 "name"），
					// 本驱动 Namer 将 DBName 大写化（"NAME"），LookUpField 无法命中。
					// 匹配后按 field 处理（列名统一用 field.DBName），同时使 Updatable/
					// convertValue/AutoUpdateTime 去重逻辑生效，避免 ORA-00957 重复列。
					for _, f := range schema.FieldsByDBName {
						if strings.EqualFold(f.DBName, key) {
							field = f
							break
						}
					}
				}
				if field != nil {
					if field.DBName != "" {
						if v, ok := selectColumns[field.DBName]; (ok && v) || (!ok && !restricted) {
							if field.Updatable {
								convertedValue := convertValue(value, field)
								sets = append(sets, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: convertedValue})
							}
						}
					}
					continue
				}
				// 无对应 field 的键：直接作为列名（GORM 语义）
				if v, ok := selectColumns[key]; (ok && v) || (!ok && !restricted) {
					sets = append(sets, clause.Assignment{Column: clause.Column{Name: key}, Value: value})
				}
			}
			// AutoUpdateTime：仅当 map 未提供该字段且未被 Omit 时自动补充
			if !stmt.SkipHooks {
				for _, field := range schema.Fields {
					if field.AutoUpdateTime > 0 && !field.PrimaryKey {
						provided := false
						for _, mapKey := range reflectValue.MapKeys() {
							k := mapKey.String()
							// 大小写不敏感比较：map 键可能是全小写 DBName（如 "updated_at"），
							// 需与 field.Name/field.DBName（本驱动大写化后为 "UPDATED_AT"）匹配，
							// 否则已提供的列会被重复补充，导致 ORA-00957 重复列名。
							if strings.EqualFold(k, field.Name) || strings.EqualFold(k, field.DBName) {
								provided = true
								break
							}
						}
						if !provided {
							if v, ok := selectColumns[field.DBName]; (ok && v) || !ok {
								sets = append(sets, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: autoUpdateTimeValue(stmt, field)})
							}
						}
					}
				}
			}
		}

		stmt.AddClause(clause.Set(sets))

		// 添加 RETURNING 子句（如果有默认值字段）
		if hasDefaultValues {
			stmt.AddClauseIfNotExists(clause.Returning{
				Columns: utils.MapFieldToColumn(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
					return clause.Column{Name: field.DBName}
				}),
			})
		}

		// 构建语句
		stmt.Build("UPDATE", "SET", "WHERE", "RETURNING")

		// 如果有 RETURNING 子句，添加 INTO 子句
		if hasDefaultValues {
			_, _ = stmt.WriteString(" INTO ")
			for idx, field := range schema.FieldsWithDefaultDBValue {
				if idx > 0 {
					_ = stmt.WriteByte(',')
				}
				boundVars[field.Name] = len(stmt.Vars)
				stmt.AddVar(stmt, outParam(field))
			}
		}
	}

	if !db.DryRun {
		// 单语句 UPDATE 天然原子，直接执行即可
		result, err := stmt.ConnPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...)
		if err != nil {
			_ = db.AddError(err)
			return
		}

		db.RowsAffected, _ = result.RowsAffected()

		// 处理 RETURNING 返回值
		if hasDefaultValues {
			updateTo := stmt.ReflectValue
			switch updateTo.Kind() {
			case reflect.Slice, reflect.Array:
				// 对于切片或数组，只更新第一个元素
				if updateTo.Len() > 0 {
					updateTo = updateTo.Index(0)
				}
			}

			// 绑定返回值到模型字段
			utils.ForEachField(
				utils.FilterFields(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) bool {
					return utils.ContainsField(boundVars, field.Name)
				}),
				func(field *gormSchema.Field) {
					switch updateTo.Kind() {
					case reflect.Struct:
						if err = field.Set(stmt.Context, updateTo, outDest(stmt.Vars, boundVars[field.Name])); err != nil {
							_ = db.AddError(err)
						}
					case reflect.Map:
						// 设置Map类型的值
						mapValue := reflect.ValueOf(updateTo.Interface())
						if mapValue.IsValid() && mapValue.Type().Key().Kind() == reflect.String {
							keyValue := reflect.ValueOf(field.DBName)
							destValue := reflect.ValueOf(outDest(stmt.Vars, boundVars[field.Name]))
							if destValue.Kind() == reflect.Pointer {
								destValue = destValue.Elem()
							}
							mapValue.SetMapIndex(keyValue, destValue)
						}
					}
				},
			)
		}
	}
}

// autoUpdateTimeValue 按字段的 AutoUpdateTime 类型生成当前时间值，
// 与 GORM ConvertToAssignments 的时间类型映射一致。
func autoUpdateTimeValue(stmt *gorm.Statement, field *gormSchema.Field) any {
	now := stmt.DB.NowFunc() //nolint:staticcheck // NowFunc 仅在 *gorm.DB 上，*gorm.Statement 无此字段
	switch field.AutoUpdateTime {
	case gormSchema.UnixNanosecond:
		return now.UnixNano()
	case gormSchema.UnixMillisecond:
		return now.UnixMilli()
	case gormSchema.UnixSecond:
		return now.Unix()
	default:
		return now
	}
}
