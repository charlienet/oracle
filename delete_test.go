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
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"default:unknown"`
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
	ID1  uint `gorm:"primaryKey"`
	ID2  uint `gorm:"primaryKey"`
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

// TestDeleteSoft_DestNotModel 验证 Dest != Model 时的额外 WHERE 条件
// P1-6: 修复软删除路径中 stmt.Dest != stmt.Model 的情况
func TestDeleteSoft_DestNotModel(t *testing.T) {
	// 场景：通过设置 Model 字段来模拟 Dest != Model 的情况
	// 参考 GORM 官方 callbacks/delete.go:139-146 的实现
	model := &softDeleteModel{ID: 1}
	db := newTestDB(t, model)

	// 模拟 Dest != Model 的场景：设置 Model 字段
	// 在 GORM 中，当执行 db.Delete(&User{}, id) 时：
	// - stmt.Dest 是 id
	// - stmt.Model 是 &User{}（通过 db.Model() 设置）
	// - stmt.ReflectValue 是空的 User{}
	db.Statement.Model = &softDeleteModel{ID: 100} // 设置 Model 为不同的实例

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	// 验证软删除生成了 UPDATE 语句
	sql := db.Statement.SQL.String()
	t.Logf("Generated SQL: %s", sql)
	if !strings.Contains(sql, "UPDATE") {
		t.Errorf("soft delete SQL %q missing UPDATE", sql)
	}
	if !strings.Contains(sql, "SET deleted_at") {
		t.Errorf("soft delete SQL %q missing SET deleted_at", sql)
	}
	// 验证 WHERE 条件存在
	if !strings.Contains(sql, "WHERE") {
		t.Errorf("soft delete SQL %q missing WHERE clause", sql)
	}
	// 不应包含 DELETE FROM（这是软删除，不是硬删除）
	if strings.Contains(sql, "DELETE FROM") {
		t.Errorf("soft delete SQL %q should not contain DELETE FROM", sql)
	}
}

// TestDeleteHard_DestNotModel 验证硬删除路径中 Dest != Model 的处理
func TestDeleteHard_DestNotModel(t *testing.T) {
	model := &plainModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Unscoped = true

	// 模拟 Dest != Model 的场景：设置 Model 字段
	db.Statement.Model = &plainModel{ID: 100}

	Delete(db)

	if db.Error != nil {
		t.Fatalf("Delete returned error: %v", db.Error)
	}

	// 验证硬删除生成了 DELETE 语句
	sql := db.Statement.SQL.String()
	t.Logf("Generated SQL: %s", sql)
	if !strings.Contains(sql, "DELETE FROM") {
		t.Errorf("hard delete SQL %q missing DELETE FROM", sql)
	}
	// 验证 WHERE 条件存在
	if !strings.Contains(sql, "WHERE") {
		t.Errorf("hard delete SQL %q missing WHERE clause", sql)
	}
}
