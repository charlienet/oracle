package tests

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

// UserWithHook 带 Hook 的测试模型
type UserWithHook struct {
	ID        uint   `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"size:100"`
	Email     string `gorm:"size:200"`
	HookLog   string `gorm:"size:500"` // 记录 Hook 执行
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (UserWithHook) TableName() string {
	return "TEST_USERS_HOOK"
}

func (u *UserWithHook) BeforeCreate(tx *gorm.DB) error {
	u.HookLog += "BeforeCreate;"
	return nil
}

func (u *UserWithHook) AfterCreate(tx *gorm.DB) error {
	u.HookLog += "AfterCreate;"
	return nil
}

func (u *UserWithHook) BeforeUpdate(tx *gorm.DB) error {
	u.HookLog += "BeforeUpdate;"
	return nil
}

func (u *UserWithHook) AfterUpdate(tx *gorm.DB) error {
	u.HookLog += "AfterUpdate;"
	return nil
}

func (u *UserWithHook) BeforeDelete(tx *gorm.DB) error {
	u.HookLog += "BeforeDelete;"
	return nil
}

func (u *UserWithHook) AfterDelete(tx *gorm.DB) error {
	u.HookLog += "AfterDelete;"
	return nil
}

func TestHooks(t *testing.T) {
	// 创建表
	if err := DB.AutoMigrate(&UserWithHook{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS_HOOK")

	// 测试 Create Hook
	user := UserWithHook{Name: "Hook Test", Email: "hook@example.com"}
	result := DB.Create(&user)
	if result.Error != nil {
		t.Fatalf("failed to create: %v", result.Error)
	}

	if user.HookLog == "" {
		t.Error("expected hooks to be called")
	}
	t.Logf("Hook log after create: %s", user.HookLog)

	// 测试 Update Hook
	user.HookLog = "" // 清空
	if err := DB.Model(&user).Update("name", "Updated Name").Error; err != nil {
		t.Fatalf("failed to update: %v", err)
	}
	t.Logf("Hook log after update: %s", user.HookLog)
	if user.HookLog == "" {
		t.Error("expected update hooks to be called")
	}

	// 测试 Delete Hook
	user.HookLog = "" // 清空
	if err := DB.Delete(&user).Error; err != nil {
		t.Fatalf("failed to delete: %v", err)
	}
	t.Logf("Hook log after delete: %s", user.HookLog)
	if user.HookLog == "" {
		t.Error("expected delete hooks to be called")
	}
}
