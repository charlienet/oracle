package clauses

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
