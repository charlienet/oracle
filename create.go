package oracle

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"

	"github.com/charlienet/go-oracle/clauses"
	"github.com/charlienet/go-oracle/utils"
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
	var needsReturning bool // 提升到外层作用域

	if !stmt.Unscoped {
		for _, c := range schema.CreateClauses {
			stmt.AddClause(c)
		}
	}

	if stmt.SQL.String() == "" {
		values := callbacks.ConvertToCreateValues(stmt)

		// 检查是否有数据需要插入
		if len(values.Values) == 0 {
			_ = db.AddError(gorm.ErrEmptySlice)
			return
		}

		onConflict, hasConflict := stmt.Clauses["ON CONFLICT"].Expression.(clause.OnConflict)
		// are all columns in value the primary fields in schema only?
		if hasConflict && utils.ContainsSliceString(
			utils.MapString(values.Columns, func(c clause.Column) string { return c.Name }),
			utils.MapString(schema.PrimaryFields, func(field *gormSchema.Field) string { return field.DBName }),
		) {
			// 构建批量 MERGE 语句
			// 使用 SELECT UNION ALL 构建多行数据源
			stmt.AddClauseIfNotExists(clauses.Merge{
				Using: []clause.Interface{
					clause.Select{
						Columns: func() []clause.Column {
							result := make([]clause.Column, len(values.Columns))
							// 对于批量操作，使用 UNION ALL 构建多行数据
							if len(values.Values) > 1 {
								// 批量 MERGE：使用 UNION ALL
								for i, column := range values.Columns {
									column.Alias = column.Name
									result[i] = column
								}
								return result
							} else {
								// 单条 MERGE：使用原来的方式
								for i, column := range values.Columns {
									buf := bytes.NewBufferString("")
									stmt.Vars = append(stmt.Vars, values.Values[0][i])
									stmt.BindVarTo(buf, stmt, nil)
									column.Alias = column.Name
									column.Name = buf.String()
									result[i] = column
								}
								return result
							}
						}(),
					},
					clause.From{
						Tables: func() []clause.Table {
							if len(values.Values) > 1 {
								// 批量 MERGE：使用 UNION ALL 构建数据源
								return []clause.Table{{Name: ""}} // 空表名，后续手动构建
							} else {
								// 单条 MERGE：使用 DUAL 表
								return []clause.Table{{Name: db.Dialector.(*Dialector).DummyTableName()}}
							}
						}(),
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
				} else {
					// 警告：filteredSet 为空时跳过 WHEN MATCHED（静默 DoNothing）
					db.Logger.Warn(stmt.Context, "oracle: MERGE UpdateAll 过滤主键后无可更新列，跳过 WHEN MATCHED（DoNothing）")
				}
			}

			stmt.AddClauseIfNotExists(clauses.WhenNotMatched{Values: values})

			// 构建并执行 MERGE 语句
			if len(values.Values) > 1 {
				// 批量 MERGE：按 chunk 分片构建 SELECT UNION ALL 并逐片执行。
				// 规避 Oracle 单语句绑定变量上限（65,535，保守按 60,000 预算）与
				// SQL 文本 64KB 限制；所有 chunk 在 ensureWriteTx 保证的同一事务内
				// 多次 Exec（事务池在分片循环外获取一次）。
				chunkSize := batchChunkSize(len(values.Columns))
				var execPool gorm.ConnPool
				var finish execFinisher
				if !db.DryRun {
					var txErr error
					execPool, finish, txErr = ensureWriteTx(stmt.Context, stmt.ConnPool, stmt.DB.Logger) //nolint:staticcheck
					if txErr != nil {
						_ = db.AddError(txErr)
						return
					}
					defer finish(db)
				}
				for start := 0; start < len(values.Values); start += chunkSize {
					end := start + chunkSize
					if end > len(values.Values) {
						end = len(values.Values)
					}
					chunk := values.Values[start:end]

					// 重置本 chunk 的 SQL 与绑定变量（序号从 :1 重新开始）
					stmt.SQL.Reset()
					stmt.Vars = stmt.Vars[:0]

					// 构建 MERGE 语句（与原有批量构建一致，仅行数按 chunk 分块）
					_, _ = stmt.WriteString("MERGE INTO ")
					_, _ = stmt.WriteString(stmt.Schema.Table)
					_, _ = stmt.WriteString(" USING (")

					// 构建 UNION ALL 查询
					for rowIdx, vals := range chunk {
						if rowIdx > 0 {
							_, _ = stmt.WriteString(" UNION ALL ")
						}
						_, _ = stmt.WriteString("SELECT ")
						for colIdx, val := range vals {
							if colIdx > 0 {
								_, _ = stmt.WriteString(", ")
							}
							stmt.AddVar(stmt, val)
							_, _ = stmt.WriteString(" AS ")
							_, _ = stmt.WriteString(values.Columns[colIdx].Name)
						}
						_, _ = stmt.WriteString(" FROM DUAL")
					}

					_, _ = stmt.WriteString(") ")
					_, _ = stmt.WriteString(clauses.MergeDefaultExcludeName())
					_, _ = stmt.WriteString(" ON (")
					for idx, field := range schema.PrimaryFields {
						if idx > 0 {
							_, _ = stmt.WriteString(" AND ")
						}
						_, _ = stmt.WriteString(stmt.Schema.Table)
						_, _ = stmt.WriteString(".")
						_, _ = stmt.WriteString(field.DBName)
						_, _ = stmt.WriteString(" = ")
						_, _ = stmt.WriteString(clauses.MergeDefaultExcludeName())
						_, _ = stmt.WriteString(".")
						_, _ = stmt.WriteString(field.DBName)
					}
					_, _ = stmt.WriteString(")")

					// WHEN MATCHED
					if len(onConflict.DoUpdates) > 0 {
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
							_, _ = stmt.WriteString(" WHEN MATCHED THEN UPDATE SET ")
							for idx, assignment := range filteredSet {
								if idx > 0 {
									_, _ = stmt.WriteString(", ")
								}
								_, _ = stmt.WriteString(stmt.Schema.Table)
								_, _ = stmt.WriteString(".")
								_, _ = stmt.WriteString(assignment.Column.Name)
								_, _ = stmt.WriteString(" = ")
								_, _ = stmt.WriteString(clauses.MergeDefaultExcludeName())
								_, _ = stmt.WriteString(".")
								_, _ = stmt.WriteString(assignment.Column.Name)
							}
							// 注：Oracle 12c+ 的 MERGE 语句不支持 RETURNING 子句
							//（实测 12.2.0.1：WHEN MATCHED/WHEN NOT MATCHED 任一分支
							// 位置均报 ORA-00933，见 supportsMergeReturning 注释），
							// 故此处不输出 RETURNING。默认值字段在 MERGE 分支
							//（主键已知）无需回填。
						}
					}

					// WHEN NOT MATCHED
					_, _ = stmt.WriteString(" WHEN NOT MATCHED THEN INSERT (")
					for idx, col := range values.Columns {
						if idx > 0 {
							_, _ = stmt.WriteString(", ")
						}
						_, _ = stmt.WriteString(col.Name)
					}
					_, _ = stmt.WriteString(") VALUES (")
					for idx, col := range values.Columns {
						if idx > 0 {
							_, _ = stmt.WriteString(", ")
						}
						_, _ = stmt.WriteString(clauses.MergeDefaultExcludeName())
						_, _ = stmt.WriteString(".")
						_, _ = stmt.WriteString(col.Name)
					}
					_, _ = stmt.WriteString(")")

					if !db.DryRun {
						result, execErr := execPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...)
						if execErr != nil {
							_ = db.AddError(execErr)
							return
						}
						rowsAffected, _ := result.RowsAffected()
						db.RowsAffected += rowsAffected
					}
				}
			} else {
				// 单条 MERGE：使用原来的方式
				// 注：Oracle 12c+ 的 MERGE 语句不支持 RETURNING 子句
				//（实测 12.2.0.1：任一分支位置均报 ORA-00933，见
				// supportsMergeReturning 注释），故此处不输出 RETURNING。
				stmt.Build("MERGE", "WHEN MATCHED", "WHEN NOT MATCHED")

				// 单条 MERGE：逐行执行（values.Values 恒为 1 行）
				if !db.DryRun {
					execSingleRowCreate(db, stmt, schema, values, boundVars, hasDefaultValues)
				}
			}
		} else {
			stmt.AddClauseIfNotExists(clause.Insert{Table: clause.Table{Name: stmt.Schema.Table}})

			// 判断是否需要 RETURNING INTO（有默认值字段需要回填）
			needsReturning = hasDefaultValues && !allDefaultFieldsHaveValues(schema.FieldsWithDefaultDBValue, values.Columns)

			// 批量插入：使用 INSERT ALL 语法
			if len(values.Values) > 1 && !needsReturning {
				// 批量 INSERT ALL：按 chunk 分片构建并逐片执行。
				// 规避 Oracle 单语句绑定变量上限（65,535，保守按 60,000 预算）与
				// SQL 文本 64KB 限制；所有 chunk 在 ensureWriteTx 保证的同一事务内
				// 多次 Exec（事务池在分片循环外获取一次）。
				chunkSize := batchChunkSize(len(values.Columns))
				var execPool gorm.ConnPool
				var finish execFinisher
				if !db.DryRun {
					var txErr error
					execPool, finish, txErr = ensureWriteTx(stmt.Context, stmt.ConnPool, stmt.DB.Logger) //nolint:staticcheck
					if txErr != nil {
						_ = db.AddError(txErr)
						return
					}
					defer finish(db)
				}
				for start := 0; start < len(values.Values); start += chunkSize {
					end := start + chunkSize
					if end > len(values.Values) {
						end = len(values.Values)
					}
					chunk := values.Values[start:end]

					// 重置本 chunk 的 SQL 与绑定变量（序号从 :1 重新开始）
					stmt.SQL.Reset()
					stmt.Vars = stmt.Vars[:0]

					// 构建 INSERT ALL 语句（与原有构建一致，仅行数按 chunk 分块）
					_, _ = stmt.WriteString("INSERT ALL")
					for _, vals := range chunk {
						_, _ = stmt.WriteString(" INTO ")
						_, _ = stmt.WriteString(stmt.Schema.Table)
						_, _ = stmt.WriteString(" (")
						for idx, col := range values.Columns {
							if idx > 0 {
								_ = stmt.WriteByte(',')
							}
							_, _ = stmt.WriteString(col.Name)
						}
						_, _ = stmt.WriteString(") VALUES (")
						for idx := range vals {
							if idx > 0 {
								_ = stmt.WriteByte(',')
							}
							stmt.AddVar(stmt, vals[idx])
						}
						_ = stmt.WriteByte(')')
					}
					_, _ = stmt.WriteString(" SELECT * FROM dual")

					if !db.DryRun {
						result, execErr := execPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...)
						if execErr != nil {
							_ = db.AddError(execErr)
							return
						}
						rowsAffected, _ := result.RowsAffected()
						db.RowsAffected += rowsAffected
					}
				}
			} else {
				// 单条插入或需要 RETURNING：使用原来的方式
				stmt.AddClause(clause.Values{Columns: values.Columns, Values: [][]any{values.Values[0]}})

				if needsReturning {
					// 有默认值字段需要回填：使用 RETURNING INTO
					stmt.AddClauseIfNotExists(clause.Returning{
						Columns: utils.MapFieldToColumn(schema.FieldsWithDefaultDBValue, func(field *gormSchema.Field) clause.Column {
							return clause.Column{Name: field.DBName}
						}),
					})
					stmt.Build("INSERT", "VALUES", "RETURNING")
					_, _ = stmt.WriteString(" INTO ")
					for idx, field := range schema.FieldsWithDefaultDBValue {
						if idx > 0 {
							_ = stmt.WriteByte(',')
						}
						boundVars[field.Name] = len(stmt.Vars)
						stmt.AddVar(stmt, outParam(field))
					}
				} else {
					// 无需回填：不使用 RETURNING
					stmt.Build("INSERT", "VALUES")
				}

				// 单条插入或需要 RETURNING：逐行执行 + 回填
				if !db.DryRun {
					execSingleRowCreate(db, stmt, schema, values, boundVars, hasDefaultValues)
				}
			}
		}
	}
}

// execSingleRowCreate 逐行执行单条 INSERT/MERGE 语句并回填 RETURNING 值。
// 用于单条插入（含需要 RETURNING 的场景）与单条 MERGE（values.Values 恒为 1 行）。
// 经 ensureWriteTx 获取写事务执行池（不在事务中时包一层自管事务保持原子性，
// 兼容 PrepareStmt 模式的 *gorm.PreparedStmtDB）。
// 注意：调用方需保证非 DryRun（DryRun 只构建 SQL 不执行）。
func execSingleRowCreate(db *gorm.DB, stmt *gorm.Statement, schema *gormSchema.Schema, values clause.Values, boundVars map[string]int, hasDefaultValues bool) {
	execPool, finish, err := ensureWriteTx(stmt.Context, stmt.ConnPool, stmt.DB.Logger) //nolint:staticcheck // Logger 仅在 *gorm.DB 上，*gorm.Statement 无此字段
	if err != nil {
		_ = db.AddError(err)
		return
	}
	defer finish(db)

	for rowIdx, vals := range values.Values {
		// 覆盖 INSERT 列对应的 Vars
		for colIdx, val := range vals {
			if colIdx >= len(values.Columns) {
				break
			}
			stmt.Vars[colIdx] = val
		}

		switch result, err := execPool.ExecContext(stmt.Context, stmt.SQL.String(), stmt.Vars...); err {
		case nil:
			rowsAffected, _ := result.RowsAffected()
			db.RowsAffected += rowsAffected

			insertTo := stmt.ReflectValue
			switch insertTo.Kind() {
			case reflect.Slice, reflect.Array:
				insertTo = insertTo.Index(rowIdx)
			}

			// 如果是指针，解引用获取底层值
			if insertTo.Kind() == reflect.Pointer {
				insertTo = insertTo.Elem()
			}

			// 回填 RETURNING INTO 的值
			if hasDefaultValues {
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
		default:
			_ = db.AddError(err)
			return
		}
	}
}

// allDefaultFieldsHaveValues 检查所有默认值字段是否都在 INSERT 列中
// 如果所有默认值字段都有显式值，则不需要 RETURNING INTO
func allDefaultFieldsHaveValues(defaultFields []*gormSchema.Field, columns []clause.Column) bool {
	if len(defaultFields) == 0 {
		return true
	}

	// 构建 columns 名称集合
	columnNames := make(map[string]bool, len(columns))
	for _, col := range columns {
		columnNames[col.Name] = true
	}

	// 检查每个默认值字段是否在 columns 中
	for _, field := range defaultFields {
		if !columnNames[field.DBName] {
			// 该默认值字段不在 INSERT 列中，需要 RETURNING INTO 回填
			return false
		}
	}

	// 所有默认值字段都在 INSERT 列中，无需 RETURNING INTO
	return true
}

// getAutoIncrementField 获取自增主键字段
func getAutoIncrementField(schema *gormSchema.Schema) *gormSchema.Field {
	for _, field := range schema.Fields {
		if field.AutoIncrement && field.PrimaryKey {
			return field
		}
	}
	return nil
}

// generateSequenceName 生成序列名（与 migrator.sequenceName 逻辑一致）
func generateSequenceName(table string) string {
	name := fmt.Sprintf("SEQ_%s", table)
	if len(name) > 30 {
		// 使用 CRC32 哈希保证唯一性（8 字符）
		hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(table)))
		name = name[:21] + "_" + hash // 21 + 1 + 8 = 30
	}
	return name
}

// batchChunkSize 计算批量写入的分片大小（行数）。
// Oracle 单语句绑定变量上限为 65,535（超出报 ORA-01745/ORA-24335），另有
// SQL 文本 64KB 限制。保守取 60,000 作为变量预算，按列数均分后向下取整，
// 下限 1。columns <= 0（防御，理论不可达）返回 1，避免除零。
func batchChunkSize(columns int) int {
	if columns <= 0 {
		return 1
	}
	size := 60000 / columns
	if size < 1 {
		return 1
	}
	return size
}
