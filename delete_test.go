package oracle

import (
	"strings"
	"testing"

	"gorm.io/gorm"
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

func TestDeleteNilStatement(t *testing.T) {
	db := &gorm.DB{Statement: &gorm.Statement{}}
	db.Statement.Schema = nil
	Delete(db)
	// 不应 panic
}

func TestDeleteNilSchema(t *testing.T) {
	db := &gorm.DB{Statement: &gorm.Statement{}}
	Delete(db)
	// 不应 panic
}

func TestDeleteExistingSQL(t *testing.T) {
	// 如果 stmt.SQL 已有内容，应跳过 SQL 构建
	model := &plainModel{ID: 1, Name: "x"}
	db := newTestDB(t, model)
	db.Statement.Unscoped = true
	db.Statement.SQL.WriteString("PRESET SQL")

	Delete(db)

	sql := db.Statement.SQL.String()
	if sql != "PRESET SQL" {
		t.Errorf("Delete should preserve existing SQL, got %q", sql)
	}
}

// softDeleteModelWithDefault 含软删除字段与默认值字段
type softDeleteModelWithDefault struct {
	ID        uint           `gorm:"primaryKey"`
	Name      string         `gorm:"default:unknown"`
	DeletedAt gorm.DeletedAt
}

func TestDeleteSoftWithDefaultValues(t *testing.T) {
	// 软删除模型含默认值字段：应生成 RETURNING INTO
	model := &softDeleteModelWithDefault{ID: 1}
	db := newTestDB(t, model)

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "UPDATE") {
		t.Errorf("soft delete SQL %q missing UPDATE", sql)
	}
}

// hardDeleteModelWithDefault 硬删除模型含默认值字段
type hardDeleteModelWithDefault struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"default:unknown"`
}

func TestDeleteHardWithDefaultValues(t *testing.T) {
	// 硬删除模型含默认值字段：应生成 RETURNING INTO
	model := &hardDeleteModelWithDefault{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Unscoped = true

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "DELETE FROM") {
		t.Errorf("hard delete SQL %q missing DELETE FROM", sql)
	}
}

// plainModelWithPK 多主键模型
type plainModelMultiPK struct {
	ID1 uint `gorm:"primaryKey"`
	ID2 uint `gorm:"primaryKey"`
	Name string
}

func TestDeleteMultiPK(t *testing.T) {
	// 多主键模型：应注入多个主键 WHERE 条件
	model := &plainModelMultiPK{ID1: 1, ID2: 2, Name: "x"}
	db := newTestDB(t, model)
	db.Statement.Unscoped = true

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "DELETE FROM") {
		t.Errorf("multi-pk delete SQL %q missing DELETE FROM", sql)
	}
}
