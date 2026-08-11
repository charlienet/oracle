package clauses

// 注意：本 clause 当前未被 create.go/update.go/delete.go 使用——
// 各文件的 RETURNING INTO 逻辑为手工内联构建（因占位符与已有 Vars 冲突）。
// 保留用于测试与文档用途；若后续需要复用，需解决占位符编号与 Vars 对齐问题。

import (
	"fmt"
	"gorm.io/gorm/clause"
)

type ReturningInto struct {
	Variables []clause.Column
	Into      []*clause.Values
}

// Name returns the name of the clause
func (r ReturningInto) Name() string {
	return "RETURNING INTO"
}

// Build builds the SQL for the RETURNING INTO clause
func (r ReturningInto) Build(builder clause.Builder) {
	if len(r.Variables) > 0 {
		builder.WriteString("RETURNING ")
		for idx, col := range r.Variables {
			if idx > 0 {
				builder.WriteByte(',')
			}
			builder.WriteQuoted(col)
		}

		builder.WriteString(" INTO ")
		for idx := range r.Variables {
			if idx > 0 {
				builder.WriteByte(',')
			}
			// 写入绑定变量占位符
			builder.WriteString(fmt.Sprintf(":%d", idx+1))
		}
	}
}

// MergeClause merge returning into clause
func (r ReturningInto) MergeClause(clause *clause.Clause) {
	clause.Name = r.Name()
	clause.Expression = r
}
