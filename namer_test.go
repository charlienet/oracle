package oracle

import (
	"testing"

	"gorm.io/gorm/schema"
)

func TestConvertNameToFormat(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "HELLO"},
		{"Hello", "HELLO"},
		{"TEST_USERS", "TEST_USERS"},
		{"test_users", "TEST_USERS"},
		{"created_at", "CREATED_AT"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ConvertNameToFormat(tt.in); got != tt.want {
				t.Errorf("ConvertNameToFormat(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func newTestNamer() Namer {
	return Namer{NamingStrategy: schema.NamingStrategy{}}
}

func TestNamerTableName(t *testing.T) {
	n := newTestNamer()

	if got := n.TableName("test_users"); got != "TEST_USERS" {
		t.Errorf("TableName() = %q, want %q", got, "TEST_USERS")
	}
}

func TestNamerTableNameWithDBName(t *testing.T) {
	n := newTestNamer()
	n.DBName = "MERCHANT"

	if got := n.TableName("test_users"); got != "MERCHANT.TEST_USERS" {
		t.Errorf("TableName() with DBName = %q, want %q", got, "MERCHANT.TEST_USERS")
	}
}

func TestNamerColumnName(t *testing.T) {
	n := newTestNamer()

	if got := n.ColumnName("test_users", "created_at"); got != "CREATED_AT" {
		t.Errorf("ColumnName() = %q, want %q", got, "CREATED_AT")
	}
}

func TestNamerJoinTableName(t *testing.T) {
	n := newTestNamer()

	if got := n.JoinTableName("order_items"); got != "ORDER_ITEMS" {
		t.Errorf("JoinTableName() = %q, want %q", got, "ORDER_ITEMS")
	}
}

func TestNamerRelationshipFKName(t *testing.T) {
	n := newTestNamer()

	rel := schema.Relationship{
		Name:   "User",
		Schema: &schema.Schema{Table: "users"},
	}

	if got := n.RelationshipFKName(rel); got != "FK_USERS_USER" {
		t.Errorf("RelationshipFKName() = %q, want %q", got, "FK_USERS_USER")
	}
}

func TestNamerCheckerName(t *testing.T) {
	n := newTestNamer()

	if got := n.CheckerName("test_users", "phone"); got != "CHK_TEST_USERS_PHONE" {
		t.Errorf("CheckerName() = %q, want %q", got, "CHK_TEST_USERS_PHONE")
	}
}

func TestNamerIndexName(t *testing.T) {
	n := newTestNamer()

	if got := n.IndexName("test_users", "name"); got != "IDX_TEST_USERS_NAME" {
		t.Errorf("IndexName() = %q, want %q", got, "IDX_TEST_USERS_NAME")
	}
}

func TestNamerSchemaName(t *testing.T) {
	n := newTestNamer()

	if got := n.SchemaName("public"); got != "PUBLIC" {
		t.Errorf("SchemaName() = %q, want %q", got, "PUBLIC")
	}
}

func TestNamerUniqueName(t *testing.T) {
	n := newTestNamer()

	if got := n.UniqueName("test_users", "email"); got != "UNI_TEST_USERS_EMAIL" {
		t.Errorf("UniqueName() = %q, want %q", got, "UNI_TEST_USERS_EMAIL")
	}
}
