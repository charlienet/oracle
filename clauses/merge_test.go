package clauses

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm/clause"
)

func TestMergeName(t *testing.T) {
	var m Merge
	if got := m.Name(); got != "MERGE" {
		t.Errorf("Merge.Name() = %q, want %q", got, "MERGE")
	}
}

func TestMergeDefaultExcludeName(t *testing.T) {
	if got := MergeDefaultExcludeName(); got != "exclude" {
		t.Errorf("MergeDefaultExcludeName() = %q, want %q", got, "exclude")
	}
}

func TestMergeBuild(t *testing.T) {
	merge := Merge{
		Table: clause.Table{Name: "users"},
		Using: []clause.Interface{
			clause.Select{Columns: []clause.Column{{Name: "id"}, {Name: "name"}}},
			clause.From{Tables: []clause.Table{{Name: "users"}}},
		},
		On: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "a"}, Value: clause.Column{Name: "b"}},
		},
	}

	sql := buildClauseSQL(t, "MERGE", merge)

	for _, want := range []string{
		"MERGE INTO",                // 前缀 + Insert 构建
		"USING (",                   // USING 子查询
		"SELECT id,name FROM users", // USING 子查询内容
		") exclude ON (",            // exclude 别名 + ON
		"a = b",                     // ON 条件
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("Merge SQL %q does not contain %q", sql, want)
		}
	}
}

func TestMergeBuildEmpty(t *testing.T) {
	var merge Merge

	sql := buildClauseSQL(t, "MERGE", merge)
	if want := "MERGE INTO users USING () exclude ON ()"; sql != want {
		t.Errorf("empty Merge SQL = %q, want %q", sql, want)
	}
}

func TestMergeMergeClause(t *testing.T) {
	merge := Merge{
		Table: clause.Table{Name: "users"},
		On: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "a"}, Value: clause.Column{Name: "b"}},
		},
	}

	cc := &clause.Clause{}
	merge.MergeClause(cc)

	if cc.Name != "MERGE" {
		t.Errorf("MergeClause name = %q, want %q", cc.Name, "MERGE")
	}

	if got, ok := cc.Expression.(Merge); !ok || !reflect.DeepEqual(got, merge) {
		t.Errorf("MergeClause expression = %#v, want %#v", cc.Expression, merge)
	}
}
