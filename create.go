package oracle

import (
	"bytes"
	"database/sql"
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"

	"github.com/charlienet/oracle/clauses"
	"github.com/charlienet/oracle/utils"
	go_ora "github.com/sijms/go-ora/v2"
)

func Create(db *gorm.DB) {
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
		for _, c := range schema.CreateClauses {
			stmt.AddClause(c)
		}
	}

	if stmt.SQL.String() == "" {
		values := callbacks.ConvertToCreateValues(stmt)
		onConflict, hasConflict := stmt.Clauses["ON CONFLICT"].Expression.(clause.OnConflict)
		// are all columns in value the primary fields in schema only?
		if hasConflict && utils.ContainsSliceString(
			utils.MapString(values.Columns, func(c clause.Column) string { return c.Name }),
			utils.MapString(schema.PrimaryFields, func(field *gormSchema.Field) string { return field.DBName }),
		) {
			stmt.AddClauseIfNotExists(clauses.Merge{
				Using: []clause.Interface{
					clause.Select{
						Columns: utils.MapClauseColumn(values.Columns, func(column clause.Column) clause.Column {
						// HACK: I can not come up with a better alternative for now
						// I want to add a value to the list of variable and then capture the bind variable position as well
						buf := bytes.NewBufferString("")
						stmt.Vars = append(stmt.Vars, values.Values[0][utils.IndexOf(values.Columns, column)])
						stmt.BindVarTo(buf, stmt, nil)

						column.Alias = column.Name
						// then the captured bind var will be the name
						column.Name = buf.String()
						return column
					}), //utils.MapClauseColumn(values.Columns, func(column clause.Column) clause.Column {
					},
					clause.From{
						Tables: []clause.Table{{Name: db.Dialector.(Dialector).DummyTableName()}},
					},
				},
				On: utils.MapFieldToExpr(schema.PrimaryFields, func(field *gormSchema.Field) clause.Expression {
					return clause.Eq{
						Column: clause.Column{Table: stmt.Schema.Table, Name: field.DBName},
						Value:  clause.Column{Table: clauses.MergeDefaultExcludeName(), Name: field.DBName},
					}
				}),
			})
			stmt.AddClauseIfNotExists(clauses.WhenMatched{Set: onConflict.DoUpdates})
			stmt.AddClauseIfNotExists(clauses.WhenNotMatched{Values: values})

			stmt.Build("MERGE", "WHEN MATCHED", "WHEN NOT MATCHED")
		} else {
			stmt.AddClauseIfNotExists(clause.Insert{Table: clause.Table{Name: stmt.Schema.Table}})
			stmt.AddClause(clause.Values{Columns: values.Columns, Values: [][]interface{}{values.Values[0]}})
			if hasDefaultValues {
			stmt.AddClauseIfNotExists(clause.Returning{
				Columns: utils.MapFieldToColumn(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
					return clause.Column{Name: field.DBName}
				}),
			})
			}
			stmt.Build("INSERT", "VALUES", "RETURNING")
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
			// 开启事务以确保批量插入的一致性
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

			for idx, vals := range values.Values {
				// HACK HACK: replace values one by one, assuming its value layout will be the same all the time, i.e. aligned
				for idx, val := range vals {
					switch v := val.(type) {
					case bool:
						if v {
							val = 1
						} else {
							val = 0
						}
					}

					stmt.Vars[idx] = val
				}
				// and then we insert each row one by one then put the returning values back (i.e. last return id => smart insert)
				// we keep track of the index so that the sub-reflected value is also correct

				var execConn *sql.Tx
				if isTransaction {
					execConn = tx // 已经在事务中，直接使用原事务
				} else {
					execConn = tx // 使用新创建的事务
				}

				switch result, err := execConn.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...); err {
				case nil: // success
					// 批量插入时累加每个单行插入的受影响行数
					rowsAffected, _ := result.RowsAffected()
					db.RowsAffected += rowsAffected

					insertTo := stmt.ReflectValue
					switch insertTo.Kind() {
					case reflect.Slice, reflect.Array:
						insertTo = insertTo.Index(idx)
					}

					if hasDefaultValues {
						// bind returning value back to reflected value in the respective fields
						utils.ForEachField(
							utils.FilterFields(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) bool {
								return utils.ContainsField(boundVars, field.Name)
							}),
							func(field *gormSchema.Field) {
								switch insertTo.Kind() {
								case reflect.Struct:
									if err = field.Set(stmt.Context, insertTo, stmt.Vars[boundVars[field.Name]].(go_ora.Out).Dest); err != nil {
										db.AddError(err)
									}
								case reflect.Map:
									// 设置Map类型的ID值
									mapValue := reflect.ValueOf(insertTo.Interface())
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
				default: // failure
					db.AddError(err)
					// 如果不是在已有事务中，则回滚我们创建的事务
					if !isTransaction {
						_ = tx.Rollback()
					}
					return
				}
			}
		}
	}
}


