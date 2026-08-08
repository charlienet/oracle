package tests

import (
	"testing"
)

// TestSoftDeleteSetsDeletedAt 精确验证软删除后 DELETED_AT 确实被写入数据库
func TestSoftDeleteSetsDeletedAt(t *testing.T) {
	// 确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	user := User{Name: "Soft Delete Exact", Email: "soft_exact@example.com", Age: 40}
	if err := DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	// 软删除
	if err := DB.Delete(&user).Error; err != nil {
		t.Fatalf("failed to soft delete: %v", err)
	}

	// 直接验证 DELETED_AT 被写入：用 Unscoped 查询（绕过软删除过滤）
	var deleted User
	if err := DB.Unscoped().First(&deleted, user.ID).Error; err != nil {
		t.Fatalf("failed to query unscoped: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Errorf("expected DeletedAt to be valid (set), got invalid")
	}
	if deleted.DeletedAt.Time.IsZero() {
		t.Errorf("expected DeletedAt to have a timestamp, got zero")
	}
	t.Logf("DeletedAt set to: %v", deleted.DeletedAt.Time)
}
