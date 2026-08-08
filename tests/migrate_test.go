package tests

import (
	"strings"
	"testing"
)

func TestAutoMigrate(t *testing.T) {
	// 测试创建表
	err := DB.AutoMigrate(&User{}, &Product{}, &Order{})
	if err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}

	// 验证表存在
	if !DB.Migrator().HasTable(&User{}) {
		t.Error("expected User table to exist")
	}
	if !DB.Migrator().HasTable(&Product{}) {
		t.Error("expected Product table to exist")
	}
	if !DB.Migrator().HasTable(&Order{}) {
		t.Error("expected Order table to exist")
	}
}

func TestAddColumn(t *testing.T) {
	// 先创建表
	DB.AutoMigrate(&User{})

	// 添加列（需要定义新模型）
	type UserWithPhone struct {
		User
		Phone string `gorm:"size:20"`
	}

	err := DB.AutoMigrate(&UserWithPhone{})
	if err != nil {
		t.Fatalf("failed to add column: %v", err)
	}

	// 验证列存在
	if !DB.Migrator().HasColumn(&UserWithPhone{}, "phone") {
		t.Error("expected phone column to exist")
	}
}

func TestDropTable(t *testing.T) {
	// 确保表存在
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	err := DB.Migrator().DropTable(&User{})
	if err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	if DB.Migrator().HasTable(&User{}) {
		t.Error("expected User table to be dropped")
	}
}

// BigStringModel 验证 11g 下 size>4000 的 string 字段映射为 CLOB 列
type BigStringModel struct {
	ID  uint   `gorm:"column:id;primaryKey"`
	Big string `gorm:"column:big;size:5000"`
}

func (BigStringModel) TableName() string {
	return "TEST_BIG_STRING"
}

// TestBigStringMapsToCLOBOn11g 验证 11g 下 size>4000 的 string 字段：
// DataTypeOf 的 32k VARCHAR2 特性（12c+ 才支持）在 11g 不触发，
// 字段应建为 CLOB，且能正常插入超过 4000 字节的长文本。
func TestBigStringMapsToCLOBOn11g(t *testing.T) {
	if err := DB.AutoMigrate(&BigStringModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() {
		DB.Migrator().DropTable(&BigStringModel{})
	}()

	// 验证列类型为 CLOB（Oracle 数据字典列名大写存储）
	var dataType string
	if err := DB.Raw("SELECT DATA_TYPE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND COLUMN_NAME = ?",
		"TEST_BIG_STRING", "BIG").Scan(&dataType).Error; err != nil {
		t.Fatalf("failed to query column type: %v", err)
	}
	if dataType != "CLOB" {
		t.Errorf("expected column type CLOB on 11g, got %q", dataType)
	}

	// 插入超过 4000 字节的长文本，验证 CLOB 列可容纳
	longText := strings.Repeat("a", 5000)
	bm := BigStringModel{Big: longText}
	if err := DB.Create(&bm).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}
	if bm.ID == 0 {
		t.Error("expected ID to be set")
	}

	// 回读验证内容完整
	var got string
	if err := DB.Raw("SELECT BIG FROM TEST_BIG_STRING WHERE id = ?", bm.ID).Scan(&got).Error; err != nil {
		t.Fatalf("failed to query back: %v", err)
	}
	if got != longText {
		t.Errorf("text roundtrip mismatch: got len=%d, want len=%d", len(got), len(longText))
	}
}
