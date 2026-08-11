package clauses

import (
	"strings"
	"testing"

	"gorm.io/gorm/clause"
)

func TestWhenNotMatchedName(t *testing.T) {
	var w WhenNotMatched
	if got := w.Name(); got != "WHEN NOT MATCHED" {
		t.Errorf("WhenNotMatched.Name() = %q, want %q", got, "WHEN NOT MATCHED")
	}
}

func TestWhenNotMatchedBuild(t *testing.T) {
	w := WhenNotMatched{
		Values: clause.Values{
			Columns: []clause.Column{{Name: "name"}, {Name: "age"}},
			Values:  [][]any{{"x", 1}},
		},
	}

	sql := buildClauseSQL(t, "WHEN NOT MATCHED", w)
	// 期望: WHEN NOT MATCHED  THEN INSERT (name,age) VALUES (:1,:2)
	for _, want := range []string{
		"WHEN NOT MATCHED",
		"THEN INSERT",
		"(name,age)",     // 列名
		"VALUES (:1,:2)", // VALUES 绑定参数
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenNotMatched SQL %q does not contain %q", sql, want)
		}
	}
}

func TestWhenNotMatchedBuildWithWhere(t *testing.T) {
	w := WhenNotMatched{
		Values: clause.Values{
			Columns: []clause.Column{{Name: "name"}},
			Values:  [][]any{{"x"}},
		},
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "deleted"}, Value: 0},
		}},
	}

	sql := buildSQL(t, w)
	for _, want := range []string{
		"THEN INSERT",
		"(name)",
		"VALUES (:1)",
		"WHERE deleted = :2",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("WhenNotMatched SQL %q does not contain %q", sql, want)
		}
	}
}

func TestWhenNotMatchedBuildEmpty(t *testing.T) {
	var w WhenNotMatched

	sql := buildSQL(t, w)
	if sql != "" {
		t.Errorf("WhenNotMatched with empty Columns should generate no SQL, got %q", sql)
	}
}

// TestWhenNotMatchedBuildPanicsOnMultipleRows 验证多行插入时按 Oracle 限制 panic。
func TestWhenNotMatchedBuildPanicsOnMultipleRows(t *testing.T) {
	w := WhenNotMatched{
		Values: clause.Values{
			Columns: []clause.Column{{Name: "name"}},
			Values:  [][]any{{"x"}, {"y"}},
		},
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for multiple insert rows")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "cannot insert more than one rows") {
			t.Errorf("unexpected panic message: %v", r)
		}
	}()

	buildSQL(t, w)
}
