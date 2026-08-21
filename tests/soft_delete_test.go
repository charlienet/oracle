package tests

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"github.com/charlienet/oracle"
)

// 测试模型定义
type SoftDeleteModel struct {
	ID        uint           `gorm:"primaryKey"`
	Name      string
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type TimeFieldModel struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string
	DeletedAt time.Time // 普通时间字段，但字段名为DeletedAt
}

type NormalModel struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string
	CreatedAt time.Time
}

func TestSoftDeleteDetection(t *testing.T) {
	// 连接数据库（这里使用内存数据库进行测试）
	dialector := oracle.Open(":memory:")
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect database: %v", err)
	}

	// 创建测试表
	err = db.AutoMigrate(&SoftDeleteModel{}, &TimeFieldModel{}, &NormalModel{})
	if err != nil {
		t.Fatalf("Failed to migrate tables: %v", err)
	}

	// 测试场景 1: 使用 gorm.DeletedAt 类型的软删除
	t.Run("GormDeletedAtSoftDelete", func(t *testing.T) {
		model := SoftDeleteModel{Name: "Test1"}
		db.Create(&model)

		// 验证记录存在
		var count int64
		db.Model(&SoftDeleteModel{}).Count(&count)
		if count != 1 {
			t.Errorf("Expected 1 record, got %d", count)
		}

		// 执行软删除
		db.Delete(&model)

		// 验证记录被软删除（在正常查询中不可见）
		db.Model(&SoftDeleteModel{}).Count(&count)
		if count != 0 {
			t.Errorf("Expected 0 records after soft delete, got %d", count)
		}

		// 验证记录实际存在于数据库（通过Unscoped查询）
		db.Unscoped().Model(&SoftDeleteModel{}).Count(&count)
		if count != 1 {
			t.Errorf("Expected 1 record with Unscoped after soft delete, got %d", count)
		}
	})

	// 测试场景 2: 使用 time.Time 类型但字段名为 DeletedAt 的软删除
	t.Run("TimeFieldAsSoftDelete", func(t *testing.T) {
		model := TimeFieldModel{Name: "Test2"}
		db.Create(&model)

		// 验证记录存在
		var count int64
		db.Model(&TimeFieldModel{}).Count(&count)
		if count != 1 {
			t.Errorf("Expected 1 record, got %d", count)
		}

		// 执行删除操作（应该被视为软删除）
		db.Delete(&model)

		// 验证记录被软删除（在正常查询中不可见）
		db.Model(&TimeFieldModel{}).Count(&count)
		if count != 0 {
			t.Errorf("Expected 0 records after soft delete, got %d", count)
		}

		// 验证记录实际存在于数据库（通过Unscoped查询）
		db.Unscoped().Model(&TimeFieldModel{}).Count(&count)
		if count != 1 {
			t.Errorf("Expected 1 record with Unscoped after soft delete, got %d", count)
		}
	})

	// 测试场景 3: 验证 Unscoped 强制硬删除
	t.Run("UnscopedHardDelete", func(t *testing.T) {
		model := SoftDeleteModel{Name: "Test3"}
		db.Create(&model)

		// 验证记录存在
		var count int64
		db.Model(&SoftDeleteModel{}).Count(&count)
		if count != 1 {
			t.Errorf("Expected 1 record, got %d", count)
		}

		// 使用 Unscoped 进行硬删除
		db.Unscoped().Delete(&model)

		// 验证记录被硬删除（在任何查询中都不可见）
		db.Model(&SoftDeleteModel{}).Count(&count)
		if count != 0 {
			t.Errorf("Expected 0 records after hard delete, got %d", count)
		}

		// 验证记录在Unscoped查询中也不可见
		db.Unscoped().Model(&SoftDeleteModel{}).Count(&count)
		if count != 0 {
			t.Errorf("Expected 0 records with Unscoped after hard delete, got %d", count)
		}
	})
}