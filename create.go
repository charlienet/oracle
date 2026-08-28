package oracle

import (
	"bytes"
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"

	"github.com/charlienet/oracle/clauses"
	"github.com/charlienet/oracle/utils"
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
						Columns: func() []clause.Column {
							result := make([]clause.Column, len(values.Columns))
							for i, column := range values.Columns {
								buf := bytes.NewBufferString("")
								stmt.Vars = append(stmt.Vars, values.Values[0][i])
								stmt.BindVarTo(buf, stmt, nil)
								column.Alias = column.Name
								column.Name = buf.String()
								result[i] = column
							}
							return result
						}(),
					},
					clause.From{
						Tables: []clause.Table{{Name: db.Dialector.(*Dialector).DummyTableName()}},
					},
				},
				On: utils.MapFieldToExpr(schema.PrimaryFields, func(field *gormSchema.Field) clause.Expression {
					return clause.Eq{
						Column: clause.Column{Table: stmt.Schema.Table, Name: field.DBName},
						Value:  clause.Column{Table: clauses.MergeDefaultExcludeName(), Name: field.DBName},
					}
				}),
			})
			// DoNothing（或空 DoUpdates）时跳过 WHEN MATCHED 子句，
			// 避免 gorm 的空 Set 兜底输出 PRIMARYKEY=PRIMARYKEY 导致语法错误
			if len(onConflict.DoUpdates) > 0 {
				// Oracle ORA-38104: UPDATE SET 不能更新 ON 子句引用的列（主键）
				pkNames := make(map[string]struct{}, len(schema.PrimaryFields))
				for _, f := range schema.PrimaryFields {
					pkNames[f.DBName] = struct{}{}
				}
				
				filteredSet := make(clause.Set, 0, len(onConflict.DoUpdates))
				for _, a := range onConflict.DoUpdates {
					if _, isPK := pkNames[a.Column.Name]; !isPK {
						filteredSet = append(filteredSet, a)
					}
				}
				
				if len(filteredSet) > 0 {
					stmt.AddClauseIfNotExists(clauses.WhenMatched{Set: filteredSet})
				}
			}
			// 检测是否为批量 MERGE
			if len(values.Values) > 1 {
				_ = db.AddError(fmt.Errorf("batch UPSERT (MERGE) is not supported, use single-row Create instead"))
				return
			}
			
			stmt.AddClauseIfNotExists(clauses.WhenNotMatched{Values: values})

			stmt.Build("MERGE", "WHEN MATCHED", "WHEN NOT MATCHED")
			// 注意：Oracle 11g 的 MERGE 语句不支持 RETURNING INTO（实测 ORA-00933）。
			// MERGE 分支仅在主键有值（columns 含主键）时触发，主键 ID 已知无需回填；
			// 非主键默认值字段（如序列默认列）在此路径下无法回填，属 Oracle 限制。
		} else {
			stmt.AddClauseIfNotExists(clause.Insert{Table: clause.Table{Name: stmt.Schema.Table}})
			stmt.AddClause(clause.Values{Columns: values.Columns, Values: [][]any{values.Values[0]}})
			if hasDefaultValues {
				stmt.AddClauseIfNotExists(clause.Returning{
					Columns: utils.MapFieldToColumn(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
						return clause.Column{Name: field.DBName}
					}),
				})
			}
			stmt.Build("INSERT", "VALUES", "RETURNING")
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
			// 获取可直接执行的连接池：不在事务中时包一层自管事务以保持批量原子性
			//（兼容 PrepareStmt 模式的 *gorm.PreparedStmtDB，见 ensureWriteTx）
			execPool, finish, err := ensureWriteTx(stmt.Context, stmt.ConnPool, stmt.DB.Logger) //nolint:staticcheck // Logger 仅在 *gorm.DB 上，*gorm.Statement 无此字段
			if err != nil {
				_ = db.AddError(err)
				return
			}
			defer finish(db)

			for rowIdx, vals := range values.Values {
				// 覆盖 INSERT 列对应的 Vars，不影响 RETURNING INTO 的输出参数
				for colIdx, val := range vals {
					if colIdx >= len(values.Columns) {
						break
					}
					stmt.Vars[colIdx] = val
				}

				switch result, err := execPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...); err {
				case nil: // success
					// 批量插入时累加每个单行插入的受影响行数
					rowsAffected, _ := result.RowsAffected()
					db.RowsAffected += rowsAffected

					insertTo := stmt.ReflectValue
					switch insertTo.Kind() {
					case reflect.Slice, reflect.Array:
						insertTo = insertTo.Index(rowIdx)
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
									if err = field.Set(stmt.Context, insertTo, outDest(stmt.Vars, boundVars[field.Name])); err != nil {
										_ = db.AddError(err)
									}
								case reflect.Map:
									// 设置Map类型的ID值
									mapValue := reflect.ValueOf(insertTo.Interface())
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
				default: // failure
					_ = db.AddError(err)
					// 事务回滚统一由 defer 处理（db.Error != nil 时执行），避免双重 Rollback
					return
				}
			}
		}
	}
}
