package oracle

import (
	"strings"
	"testing"
)

func TestDeleteHardSQLGeneration(t *testing.T) {
	// Unscoped 硬删除：应生成 DELETE FROM ... WHERE
	model := &plainModel{ID: 1, Name: "x"}
	db := newTestDB(t, model)
	db.Statement.Unscoped = true

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"DELETE FROM", "WHERE"} {
		if !strings.Contains(sql, want) {
			t.Errorf("hard delete SQL %q missing %q", sql, want)
		}
	}
}

func TestDeleteSoftSQLGeneration(t *testing.T) {
	// 模型含 gorm.DeletedAt 字段：软删除应生成 UPDATE ... SET deleted_at = ... WHERE
	model := &softDeleteModel{ID: 1}
	db := newTestDB(t, model)

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "UPDATE") {
		t.Errorf("soft delete SQL %q missing UPDATE", sql)
	}
	if !strings.Contains(sql, "SET deleted_at") {
		t.Errorf("soft delete SQL %q missing SET deleted_at", sql)
	}
	if !strings.Contains(sql, "WHERE") {
		t.Errorf("soft delete SQL %q missing WHERE", sql)
	}
	if strings.Contains(sql, "DELETE FROM") {
		t.Errorf("soft delete SQL %q should not contain DELETE FROM", sql)
	}
}

func TestDeleteMissingWhereError(t *testing.T) {
	// 主键无值且未提供 WHERE 时应报错
	model := &plainModel{Name: "x"}
	db := newTestDB(t, model)

	Delete(db)

	if db.Error == nil {
		t.Fatal("expected error for missing WHERE condition, got nil")
	}
	if !strings.Contains(db.Error.Error(), "missing WHERE") {
		t.Errorf("error %q does not contain %q", db.Error.Error(), "missing WHERE")
	}
}
