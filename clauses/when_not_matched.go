package clauses

import (
	"gorm.io/gorm/clause"
)

type WhenNotMatched struct {
	clause.Values
	Where clause.Where
}

func (w WhenNotMatched) Name() string {
	return "WHEN NOT MATCHED"
}

// MergeClause 保存 WhenNotMatched 自身，避免 gorm 调用内嵌 clause.Values 的提升
// MergeClause 导致 Expression 被覆盖为 clause.Values 且 clause.Name 被清空。
func (w WhenNotMatched) MergeClause(clause *clause.Clause) {
	clause.Name = w.Name()
	clause.Expression = w
}

// Build 构建 WHEN NOT MATCHED 子句的剩余部分。
// 配合 gorm 的 clause.Clause.Build（先输出 "WHEN NOT MATCHED " 前缀），
// 此处只输出 "THEN INSERT ..."。
func (w WhenNotMatched) Build(builder clause.Builder) {
	if len(w.Columns) > 0 {
		if len(w.Values.Values) != 1 {
			panic("cannot insert more than one rows due to Oracle SQL language restriction")
		}

		builder.WriteString("THEN INSERT ")
		w.Values.Build(builder)

		if len(w.Where.Exprs) > 0 {
			builder.WriteString(" WHERE ")
			w.Where.Build(builder)
		}
	}
}
