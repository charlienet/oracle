package tests

import (
	"testing"
)

func TestDeleteSingle(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 创建测试数据
	user := User{Name: "Delete Test", Email: "delete@example.com", Age: 40}
	DB.Create(&user)

	// 删除
	result := DB.Delete(&user)
	if result.Error != nil {
		t.Fatalf("failed to delete: %v", result.Error)
	}

	// 验证软删除（如果有 deleted_at）
	var deleted User
	result = DB.First(&deleted, user.ID)
	if result.Error == nil {
		t.Error("expected user to be soft deleted")
	}
}

func TestDeleteWithoutWhere(t *testing.T) {
	// 确保表存在
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 测试无 WHERE 条件的删除应该失败
	result := DB.Delete(&User{})
	if result.Error == nil {
		t.Error("expected error for delete without WHERE condition")
	}
	t.Logf("Got expected error: %v", result.Error)
}

func TestHardDelete(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 创建测试数据
	user := User{Name: "Hard Delete", Email: "hard@example.com", Age: 50}
	DB.Create(&user)

	// 硬删除
	result := DB.Unscoped().Delete(&user)
	if result.Error != nil {
		t.Fatalf("failed to hard delete: %v", result.Error)
	}

	// 验证完全删除
	var deleted User
	result = DB.Unscoped().First(&deleted, user.ID)
	if result.Error == nil {
		t.Error("expected user to be completely deleted")
	}
}
