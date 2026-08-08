package clauses

import (
	"reflect"
	"testing"

	"gorm.io/gorm/clause"
)

func TestReturningIntoName(t *testing.T) {
	var r ReturningInto
	if got := r.Name(); got != "RETURNING INTO" {
		t.Errorf("ReturningInto.Name() = %q, want %q", got, "RETURNING INTO")
	}
}

func TestReturningIntoBuild(t *testing.T) {
	r := ReturningInto{
		Variables: []clause.Column{{Name: "col1"}, {Name: "col2"}},
	}

	sql := buildSQL(t, r)
	if want := "RETURNING col1,col2 INTO :1,:2"; sql != want {
		t.Errorf("ReturningInto SQL = %q, want %q", sql, want)
	}
}

func TestReturningIntoBuildSingle(t *testing.T) {
	r := ReturningInto{
		Variables: []clause.Column{{Name: "col1"}},
	}

	sql := buildSQL(t, r)
	if want := "RETURNING col1 INTO :1"; sql != want {
		t.Errorf("ReturningInto SQL = %q, want %q", sql, want)
	}
}

func TestReturningIntoBuildEmpty(t *testing.T) {
	var r ReturningInto

	sql := buildSQL(t, r)
	if sql != "" {
		t.Errorf("ReturningInto with empty Variables should generate no SQL, got %q", sql)
	}
}

func TestReturningIntoMergeClause(t *testing.T) {
	r := ReturningInto{
		Variables: []clause.Column{{Name: "id"}},
	}

	cc := &clause.Clause{}
	r.MergeClause(cc)

	if cc.Name != "RETURNING INTO" {
		t.Errorf("ReturningInto.MergeClause name = %q, want %q", cc.Name, "RETURNING INTO")
	}

	if got, ok := cc.Expression.(ReturningInto); !ok || !reflect.DeepEqual(got, r) {
		t.Errorf("ReturningInto.MergeClause expression = %#v, want %#v", cc.Expression, r)
	}
}
