package oracle

import (
	"database/sql"
	"fmt"
	"reflect"

	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"
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
			db.AddError(fmt.Errorf("missing WHERE condition in UPDATE"))
			return
		}
	} else {
		// 没有 WHERE 子句，且模型主键没有可用的值
		db.AddError(fmt.Errorf("missing WHERE condition in UPDATE"))
		return
	}

	if stmt.SQL.String() == "" {
		// 构建 UPDATE 语句
		stmt.AddClauseIfNotExists(clause.Update{Table: clause.Table{Name: stmt.Schema.Table}})
		
		// 构建 SET 子句
		_, hasSet := stmt.Clauses["SET"].Expression.(clause.Set)
		if !hasSet {
			// 获取要更新的值
			// 从 stmt.Dest 获取待更新的数据
			reflectValue := reflect.ValueOf(stmt.Dest)
			if reflectValue.Kind() == reflect.Ptr {
				reflectValue = reflectValue.Elem()
			}
			
			// 构建 SET 表达式
			sets := make(clause.Set, 0)
			switch reflectValue.Kind() {
			case reflect.Struct:
				for _, field := range schema.Fields {
					if !field.PrimaryKey && field.Updatable {
						if fieldValue, isZero := field.ValueOf(stmt.Context, reflectValue); !isZero {
							// 转换值为 Oracle 兼容格式
							convertedValue := convertValue(fieldValue, field)
							sets = append(sets, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: convertedValue})
						}
					}
				}
			case reflect.Map:
				// 处理 map 类型的更新
				for _, mapKey := range reflectValue.MapKeys() {
					key := mapKey.String()
					if field := schema.LookUpField(key); field != nil {
						if !field.PrimaryKey && field.Updatable {
							value := reflectValue.MapIndex(mapKey).Interface()
							// 转换值为 Oracle 兼容格式
							convertedValue := convertValue(value, field)
							sets = append(sets, clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: convertedValue})
						}
					}
				}
			}
			
			stmt.AddClause(clause.Set(sets))
		}
		
		// 添加 RETURNING 子句（如果有默认值字段）
		if hasDefaultValues {
			stmt.AddClauseIfNotExists(clause.Returning{
				Columns: funk.Map(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
					return clause.Column{Name: field.DBName}
				}).([]clause.Column),
			})
		}
		
		// 构建语句
		stmt.Build("UPDATE", "SET", "WHERE", "RETURNING")
		
		// 如果有 RETURNING 子句，添加 INTO 子句
		if hasDefaultValues {
			stmt.WriteString(" INTO ")
			for idx, field := range schema.FieldsWithDefaultDBValue {
				if idx > 0 {
					stmt.WriteByte(',')
				}
				boundVars[field.Name] = len(stmt.Vars)
				stmt.AddVar(stmt, sql.Out{Dest: reflect.New(field.FieldType).Interface()})
			}
		}
	}

	if !db.DryRun {
		// 开启事务以确保更新的一致性
		var tx *sql.Tx
		var err error
		var isTransaction bool = false
		
		// 检查是否已经在一个事务中
		if sqlTx, ok := stmt.ConnPool.(*sql.Tx); ok {
			tx = sqlTx
			isTransaction = true
		} else if sqlDb, ok := stmt.ConnPool.(*sql.DB); ok {
			tx, err = sqlDb.Begin()
			if err != nil {
				db.AddError(err)
				return
			}
			defer func() {
				if db.Error != nil && !isTransaction {
					_ = tx.Rollback()
				} else if !isTransaction {
					_ = tx.Commit()
				}
			}()
		} else {
			db.AddError(fmt.Errorf("unsupported connection pool type"))
			return
		}

		// 执行更新操作
		var execConn *sql.Tx
		if isTransaction {
			execConn = tx // 已经在事务中，直接使用原事务
		} else {
			execConn = tx // 使用新创建的事务
		}

		result, err := execConn.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...)
		if err != nil {
			db.AddError(err)
			// 如果不是在已有事务中，则回滚我们创建的事务
			if !isTransaction {
				_ = tx.Rollback()
			}
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
			funk.ForEach(
				funk.Filter(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) bool {
					return funk.Contains(boundVars, field.Name)
				}),
				func(field *gormSchema.Field) {
					switch updateTo.Kind() {
					case reflect.Struct:
						if err = field.Set(stmt.Context, updateTo, stmt.Vars[boundVars[field.Name]].(sql.Out).Dest); err != nil {
							db.AddError(err)
						}
					case reflect.Map:
						// 设置Map类型的值
						mapValue := reflect.ValueOf(updateTo.Interface())
						if mapValue.IsValid() && mapValue.Type().Key().Kind() == reflect.String {
							keyValue := reflect.ValueOf(field.DBName)
							destValue := reflect.ValueOf(stmt.Vars[boundVars[field.Name]].(sql.Out).Dest)
							if destValue.Kind() == reflect.Ptr {
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