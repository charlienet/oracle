package oracle

import (
	"strings"
	"testing"
)

// updateTestModel 含主键与可更新字段，用于 Update 回调 SQL 生成测试
type updateTestModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string
	Age  int
}

func TestUpdateSQLGeneration(t *testing.T) {
	// 设置主键值后应生成 UPDATE ... SET ... WHERE
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"UPDATE", "SET", "WHERE"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Update SQL %q missing %q", sql, want)
		}
	}
}

func TestUpdateMissingWhereError(t *testing.T) {
	// 主键无值且未提供 WHERE 时应报错
	model := &updateTestModel{Name: "bob"}
	db := newTestDB(t, model)

	Update(db)

	if db.Error == nil {
		t.Fatal("expected error for missing WHERE condition, got nil")
	}
	if !strings.Contains(db.Error.Error(), "missing WHERE") {
		t.Errorf("error %q does not contain %q", db.Error.Error(), "missing WHERE")
	}
}
