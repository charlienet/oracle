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
	// 期望: THEN UPDATE SET name=:1（"WHEN MATCHED" 前缀由 gorm 的 clause.Clause.Build 输出）
	for _, want := range []string{
		"THEN UPDATE",
		"SET name=:1",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenMatched SQL %q does not contain %q", sql, want)
		}
	}
	if strings.Contains(sql, "WHEN MATCHED") {
		t.Errorf("WhenMatched direct Build should not repeat clause name, got %q", sql)
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
	// 期望: THEN UPDATE SET name=:1 WHERE id = :2 DELETE WHERE flag = :3
	for _, want := range []string{
		"THEN UPDATE",
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

func TestWhenMatchedBuildWithExcludedAlias(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{
			{Column: clause.Column{Name: "name"}, Value: clause.Column{Table: "excluded", Name: "name"}},
			{Column: clause.Column{Name: "age"}, Value: clause.Column{Table: "excluded", Name: "age"}},
		},
	}

	sql := buildSQL(t, w)
	
	// 验证 excluded 别名被正确替换为 exclude
	if strings.Contains(sql, `"excluded"."name`) || strings.Contains(sql, `excluded."name`) {
		t.Errorf("WhenMatched SQL should not contain 'excluded' alias, got %q", sql)
	}
	if strings.Contains(sql, `"excluded"."age`) || strings.Contains(sql, `excluded."age`) {
		t.Errorf("WhenMatched SQL should not contain 'excluded' alias, got %q", sql)
	}
	
	// 验证使用了正确的 exclude 别名 (测试实际输出格式)
	if !strings.Contains(sql, "exclude.name") {
		t.Errorf("WhenMatched SQL should contain 'exclude.name', got %q", sql)
	}
	if !strings.Contains(sql, "exclude.age") {
		t.Errorf("WhenMatched SQL should contain 'exclude.age', got %q", sql)
	}
}

func TestWhenMatchedBuildWithExcludedExpr(t *testing.T) {
	w := WhenMatched{
		Set: clause.Set{
			{Column: clause.Column{Name: "count"}, Value: clause.Expr{SQL: "excluded.count + 1"}},
		},
	}

	sql := buildSQL(t, w)
	
	// 验证 excluded 被替换为 exclude
	if strings.Contains(sql, "excluded.") {
		t.Errorf("WhenMatched SQL should not contain 'excluded.' alias, got %q", sql)
	}
	if !strings.Contains(sql, "exclude.count") {
		t.Errorf("WhenMatched SQL should contain 'exclude.count', got %q", sql)
	}
}
