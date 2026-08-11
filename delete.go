package oracle

import (
	"database/sql"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"

	"github.com/charlienet/oracle/utils"
	go_ora "github.com/sijms/go-ora/v2"
)

func Delete(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil {
		return
	}
	schema := stmt.Schema
	if schema == nil {
		return
	}

	boundVars := make(map[string]int)

	// 注入主键 WHERE 条件（GORM 默认回调会做这一步）
	addPrimaryKeyWhere(stmt, schema)

	// 1. WHERE 安全检查（最重要）
	if where, ok := stmt.Clauses["WHERE"].Expression.(clause.Where); ok {
		if checkMissingWhereConditions(where.Exprs, schema) {
			db.AddError(fmt.Errorf("missing WHERE condition in DELETE"))
			return
		}
	} else {
		// 没有 WHERE 子句
		db.AddError(fmt.Errorf("missing WHERE condition in DELETE"))
		return
	}

	// 2. 检查软删除
	var softDeleteField *gormSchema.Field
	for _, field := range schema.Fields {
		if (field.DBName == "deleted_at" || field.Name == "DeletedAt") && 
		   field.GORMDataType == "time" {
			softDeleteField = field
			break
		}
	}

	if softDeleteField != nil && !stmt.Unscoped {
		// 软删除：转换为 UPDATE
		performSoftDelete(db, softDeleteField, boundVars)
	} else {
		// 硬删除：执行 DELETE（Unscoped 时强制硬删除）
		performHardDelete(db, boundVars)
	}
}

func performSoftDelete(db *gorm.DB, field *gormSchema.Field, boundVars map[string]int) {
	stmt := db.Statement
	schema := stmt.Schema
	
	hasDefaultValues := len(schema.FieldsWithDefaultDBValue) > 0
	
	if !stmt.Unscoped {
		for _, c := range schema.DeleteClauses {
			stmt.AddClause(c)
		}
	}
	
	if stmt.SQL.String() == "" {
		// 构建 UPDATE 语句而不是 DELETE
		stmt.AddClauseIfNotExists(clause.Update{Table: clause.Table{Name: stmt.Schema.Table}})
		
		// 构建 SET 子句，设置 deleted_at 为当前时间
		now := time.Now()
		convertedNow := convertValue(now, field)
		set := clause.Set{clause.Assignment{Column: clause.Column{Name: field.DBName}, Value: convertedNow}}
		stmt.AddClause(set)
		
		// 添加 RETURNING 子句（如果有默认值字段或需要返回值）
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
			stmt.WriteString(" INTO ")
			for idx, field := range schema.FieldsWithDefaultDBValue {
				if idx > 0 {
					stmt.WriteByte(',')
				}
				boundVars[field.Name] = len(stmt.Vars)
				stmt.AddVar(stmt, go_ora.Out{Dest: reflect.New(field.FieldType).Interface(), Size: outParamSize(field)})
			}
		}
	}

	if !db.DryRun {
		// 执行软删除操作
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
			utils.ForEachField(
				utils.FilterFields(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) bool {
					return utils.ContainsField(boundVars, field.Name)
				}),
				func(field *gormSchema.Field) {
					switch updateTo.Kind() {
					case reflect.Struct:
						if err = field.Set(stmt.Context, updateTo, stmt.Vars[boundVars[field.Name]].(go_ora.Out).Dest); err != nil {
							db.AddError(err)
						}
					case reflect.Map:
						// 设置Map类型的值
						mapValue := reflect.ValueOf(updateTo.Interface())
						if mapValue.IsValid() && mapValue.Type().Key().Kind() == reflect.String {
							keyValue := reflect.ValueOf(field.DBName)
							destValue := reflect.ValueOf(stmt.Vars[boundVars[field.Name]].(go_ora.Out).Dest)
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

func performHardDelete(db *gorm.DB, boundVars map[string]int) {
	stmt := db.Statement
	schema := stmt.Schema
	
	hasDefaultValues := len(schema.FieldsWithDefaultDBValue) > 0
	
	if !stmt.Unscoped {
		for _, c := range schema.DeleteClauses {
			stmt.AddClause(c)
		}
	}
	
	if stmt.SQL.String() == "" {
		// 构建 DELETE 语句
		stmt.AddClauseIfNotExists(clause.Delete{})
		stmt.AddClauseIfNotExists(clause.From{Tables: []clause.Table{{Name: stmt.Schema.Table}}})
		
		// 添加 RETURNING 子句（如果有默认值字段或需要返回值）
		if hasDefaultValues {
			stmt.AddClauseIfNotExists(clause.Returning{
				Columns: utils.MapFieldToColumn(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
					return clause.Column{Name: field.DBName}
				}),
			})
		}
		
		// 构建语句
		stmt.Build("DELETE", "FROM", "WHERE", "RETURNING")
		
		// 如果有 RETURNING 子句，添加 INTO 子句
		if hasDefaultValues {
			stmt.WriteString(" INTO ")
			for idx, field := range schema.FieldsWithDefaultDBValue {
				if idx > 0 {
					stmt.WriteByte(',')
				}
				boundVars[field.Name] = len(stmt.Vars)
				stmt.AddVar(stmt, go_ora.Out{Dest: reflect.New(field.FieldType).Interface(), Size: outParamSize(field)})
			}
		}
	}

	if !db.DryRun {
		// 执行删除操作
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
			deleteTo := stmt.ReflectValue
			switch deleteTo.Kind() {
			case reflect.Slice, reflect.Array:
				// 对于切片或数组，只处理第一个元素
				if deleteTo.Len() > 0 {
					deleteTo = deleteTo.Index(0)
				}
			}

			// 绑定返回值到模型字段
			utils.ForEachField(
				utils.FilterFields(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) bool {
					return utils.ContainsField(boundVars, field.Name)
				}),
				func(field *gormSchema.Field) {
					switch deleteTo.Kind() {
					case reflect.Struct:
						if err = field.Set(stmt.Context, deleteTo, stmt.Vars[boundVars[field.Name]].(go_ora.Out).Dest); err != nil {
							db.AddError(err)
						}
					case reflect.Map:
						// 设置Map类型的值
						mapValue := reflect.ValueOf(deleteTo.Interface())
						if mapValue.IsValid() && mapValue.Type().Key().Kind() == reflect.String {
							keyValue := reflect.ValueOf(field.DBName)
							destValue := reflect.ValueOf(stmt.Vars[boundVars[field.Name]].(go_ora.Out).Dest)
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