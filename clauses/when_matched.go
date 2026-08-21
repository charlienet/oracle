package clauses

import (
	"gorm.io/gorm/clause"
)

type WhenMatched struct {
	clause.Set
	Where, Delete clause.Where
}

func (w WhenMatched) Name() string {
	return "WHEN MATCHED"
}

// MergeClause 保存 WhenMatched 自身，避免 gorm 调用内嵌 clause.Set 的提升
// MergeClause 导致 Expression 被覆盖为 clause.Set。
func (w WhenMatched) MergeClause(clause *clause.Clause) {
	clause.Name = w.Name()
	clause.Expression = w
}

// Build 构建 WHEN MATCHED 子句的剩余部分。
// 配合 gorm 的 clause.Clause.Build（先输出 "WHEN MATCHED " 前缀），
// 此处只输出 "THEN UPDATE SET ..."。
func (w WhenMatched) Build(builder clause.Builder) {
	if len(w.Set) > 0 {
		builder.WriteString("THEN UPDATE SET ")
		
		// 修复：将 GORM 原生的 "excluded" 别名替换为 Oracle 的 "exclude"
		for i := range w.Set {
			if col, ok := w.Set[i].Value.(clause.Column); ok && col.Table == "excluded" {
				col.Table = MergeDefaultExcludeName()
				w.Set[i].Value = col
			}
		}
		
		w.Set.Build(builder)

		if len(w.Where.Exprs) > 0 {
			builder.WriteString(" WHERE ")
			w.Where.Build(builder)
		}

		if len(w.Delete.Exprs) > 0 {
			builder.WriteString(" DELETE WHERE ")
			w.Delete.Build(builder)
		}
	}
}
