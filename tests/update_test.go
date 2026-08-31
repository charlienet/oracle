package tests

import (
	"testing"
)

func TestUpdateSingle(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearUserTables(t)

	// 创建测试数据
	user := User{Name: "Update Test", Email: "update@example.com", Age: 30}
	DB.Create(&user)

	// 更新
	result := DB.Model(&user).Update("age", 31)
	if result.Error != nil {
		t.Fatalf("failed to update: %v", result.Error)
	}

	if result.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", result.RowsAffected)
	}

	// 验证更新
	var updated User
	DB.First(&updated, user.ID)
	if updated.Age != 31 {
		t.Errorf("expected age 31, got %d", updated.Age)
	}
}

func TestUpdateMultiple(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearUserTables(t)

	// 创建测试数据
	users := []User{
		{Name: "Multi Update 1", Email: "multi1@example.com", Age: 25},
		{Name: "Multi Update 2", Email: "multi2@example.com", Age: 25},
	}
	DB.Create(&users)

	// 批量更新
	result := DB.Model(&User{}).Where("age = ?", 25).Update("age", 26)
	if result.Error != nil {
		t.Fatalf("failed to update: %v", result.Error)
	}

	if result.RowsAffected < 2 {
		t.Errorf("expected at least 2 rows affected, got %d", result.RowsAffected)
	}
}

func TestUpdateWithoutWhere(t *testing.T) {
	// 确保表存在
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearUserTables(t)

	// 测试无 WHERE 条件的更新应该失败
	result := DB.Model(&User{}).Update("age", 99)
	if result.Error == nil {
		t.Error("expected error for update without WHERE condition")
	}
	t.Logf("Got expected error: %v", result.Error)
}
