package clauses

import (
	"strings"
	"testing"

	"gorm.io/gorm/clause"
)

func TestWhenMatchedName(t *testing.T) {
	var w WhenMatched
	if got := w.Name(); got != "WHEN MATCHED" {
		t.Errorf("WhenMatched.Name() = %q, want %q", got, "WHEN MATCHED")
	}
}

func TestWhenMatchedBuild(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{{Column: clause.Column{Name: "name"}, Value: "x"}},
	}

	sql := buildSQL(t, w)
	// 期望: THEN UPDATE WHEN MATCHED SET name=:1
	for _, want := range []string{
		"THEN UPDATE",
		"WHEN MATCHED",
		"SET name=:1",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenMatched SQL %q does not contain %q", sql, want)
		}
	}
}

func TestWhenMatchedBuildMultipleSet(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{
			{Column: clause.Column{Name: "name"}, Value: "x"},
			{Column: clause.Column{Name: "age"}, Value: 1},
		},
	}

	sql := buildSQL(t, w)
	if want := "SET name=:1,age=:2"; !strings.Contains(sql, want) {
		t.Errorf("WhenMatched SQL %q does not contain %q", sql, want)
	}
}

func TestWhenMatchedBuildWithWhereAndDelete(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{{Column: clause.Column{Name: "name"}, Value: "x"}},
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "id"}, Value: 1},
		}},
		Delete: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "flag"}, Value: 0},
		}},
	}

	sql := buildSQL(t, w)
	// 期望: THEN UPDATE WHEN MATCHED SET name=:1WHERE id = :2 DELETE WHERE flag = :3
	for _, want := range []string{
		"THEN UPDATE",
		"WHEN MATCHED",
		"SET name=:1",
		"WHERE id = :2",
		"DELETE WHERE flag = :3",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenMatched SQL %q does not contain %q", sql, want)
		}
	}
}

// TestWhenMatchedClauseBuild 验证通过 clause.Clause（gorm 真实构建流程）时的完整输出。
func TestWhenMatchedClauseBuild(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{{Column: clause.Column{Name: "name"}, Value: "x"}},
	}

	sql := buildClauseSQL(t, "WHEN MATCHED", w)
	for _, want := range []string{
		"WHEN MATCHED",
		"THEN UPDATE",
		"SET name=:1",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenMatched SQL %q does not contain %q", sql, want)
		}
	}
}

func TestWhenMatchedBuildEmptySet(t *testing.T) {
	var w WhenMatched

	sql := buildSQL(t, w)
	if sql != "" {
		t.Errorf("WhenMatched with empty Set should generate no SQL, got %q", sql)
	}
}
