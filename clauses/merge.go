package clauses

import (
	"gorm.io/gorm/clause"
)

type Merge struct {
	Table clause.Table
	Using []clause.Interface
	On    []clause.Expression
}

func (merge Merge) Name() string {
	return "MERGE"
}

func MergeDefaultExcludeName() string {
	return "exclude"
}

// Build build from clause
func (merge Merge) Build(builder clause.Builder) {
	clause.Insert{}.Build(builder)
	_, _ = builder.WriteString(" USING (")
	for idx, iface := range merge.Using {
		if idx > 0 {
			_ = builder.WriteByte(' ')
		}
		_, _ = builder.WriteString(iface.Name())
		_ = builder.WriteByte(' ')
		iface.Build(builder)
	}
	_, _ = builder.WriteString(") ")
	_, _ = builder.WriteString(MergeDefaultExcludeName())
	_, _ = builder.WriteString(" ON (")
	for idx, on := range merge.On {
		if idx > 0 {
			_, _ = builder.WriteString(" AND ")
		}
		on.Build(builder)
	}
	_, _ = builder.WriteString(")")
}

// MergeClause merge values clauses
func (merge Merge) MergeClause(clause *clause.Clause) {
	clause.Name = merge.Name()
	clause.Expression = merge
}
