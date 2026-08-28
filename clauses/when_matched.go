package clauses

import (
	"regexp"

	"gorm.io/gorm/clause"
)

// excludedAliasRegex 匹配完整的 excluded. 标识符，避免误伤包含 excluded 的列名
var excludedAliasRegex = regexp.MustCompile(`(?i)\bexcluded\.`)

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
		_, _ = builder.WriteString("THEN UPDATE SET ")
		
		// 修复：将 GORM 原生的 "excluded" 别名替换为 Oracle 的 "exclude"
		for i := range w.Set {
			switch v := w.Set[i].Value.(type) {
			case clause.Column:
				if v.Table == "excluded" {
					v.Table = MergeDefaultExcludeName()
					w.Set[i].Value = v
				}
			case clause.Expr:
				// 处理 gorm.Expr() 和 clause.Expr{} 中的 excluded 引用
				// 使用正则表达式匹配完整的标识符，避免误伤
				if excludedAliasRegex.MatchString(v.SQL) {
					v.SQL = excludedAliasRegex.ReplaceAllString(v.SQL, MergeDefaultExcludeName()+".")
					w.Set[i].Value = v
				}
			}
		}
		
		w.Set.Build(builder)

		if len(w.Where.Exprs) > 0 {
			_, _ = builder.WriteString(" WHERE ")
			w.Where.Build(builder)
		}

		if len(w.Delete.Exprs) > 0 {
			_, _ = builder.WriteString(" DELETE WHERE ")
			w.Delete.Build(builder)
		}
	}
}
