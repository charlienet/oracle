package tests

import (
	"gorm.io/gorm/clause"
	"testing"
)

// MerchantMore 是一个具有复合主键的测试模型
type MerchantMore struct {
	ApplyNumber string `gorm:"column:APPLY_NUMBER;primaryKey"`
	MerchantID  string `gorm:"column:MERCHANT_ID;primaryKey"`
	Name        string `gorm:"column:NAME"`
	Address     string `gorm:"column:ADDRESS"`
}

func (MerchantMore) TableName() string {
	return "T_MERCHANT_MORE"
}

func TestOnConflictDoUpdateWithPrimaryKeyFilter(t *testing.T) {
	// 这个测试验证当 DoUpdates 包含主键列时，它们会被正确过滤掉
	// 以避免 Oracle 的 ORA-38104 错误
	
	if err := DB.AutoMigrate(&MerchantMore{}); err != nil {
		t.Skipf("Skipping test due to migration error: %v", err)
	}
	
	clearTable(t, "T_MERCHANT_MORE")

	// 创建初始记录
	initialRecord := MerchantMore{
		ApplyNumber: "APP001",
		MerchantID:  "MCH001",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 准备冲突更新，尝试更新包括主键在内的字段
	conflictRecord := MerchantMore{
		ApplyNumber: "APP001", // 主键，不应被更新
		MerchantID:  "MCH001", // 主键，不应被更新
		Name:        "Updated Name",
		Address:     "Updated Address",
	}

	// 使用 OnConflict 进行插入，如果存在则更新非主键字段
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "APPLY_NUMBER"}, Value: conflictRecord.ApplyNumber}, // 主键，应该被过滤
			{Column: clause.Column{Name: "MERCHANT_ID"}, Value: conflictRecord.MerchantID},   // 主键，应该被过滤
			{Column: clause.Column{Name: "NAME"}, Value: conflictRecord.Name},               // 非主键，应该被更新
			{Column: clause.Column{Name: "ADDRESS"}, Value: conflictRecord.Address},         // 非主键，应该被更新
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	// 验证记录确实被更新了（非主键字段）
	var updatedRecord MerchantMore
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP001", "MCH001").First(&updatedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch updated record: %v", err)
	}

	if updatedRecord.Name != "Updated Name" {
		t.Errorf("Expected Name to be 'Updated Name', got '%s'", updatedRecord.Name)
	}

	if updatedRecord.Address != "Updated Address" {
		t.Errorf("Expected Address to be 'Updated Address', got '%s'", updatedRecord.Address)
	}

	// 主键应该保持不变
	if updatedRecord.ApplyNumber != "APP001" {
		t.Errorf("Primary key ApplyNumber should remain 'APP001', got '%s'", updatedRecord.ApplyNumber)
	}

	if updatedRecord.MerchantID != "MCH001" {
		t.Errorf("Primary key MerchantID should remain 'MCH001', got '%s'", updatedRecord.MerchantID)
	}
}

func TestOnConflictDoUpdateOnlyPrimaryKeys(t *testing.T) {
	// 这个测试验证当 DoUpdates 只包含主键列时，不会生成 WHEN MATCHED 子句
	if err := DB.AutoMigrate(&MerchantMore{}); err != nil {
		t.Skipf("Skipping test due to migration error: %v", err)
	}
	
	clearTable(t, "T_MERCHANT_MORE")

	// 创建初始记录
	initialRecord := MerchantMore{
		ApplyNumber: "APP002",
		MerchantID:  "MCH002",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 准备冲突更新，只尝试更新主键字段（这些应该被过滤掉）
	conflictRecord := MerchantMore{
		ApplyNumber: "APP002",
		MerchantID:  "MCH002",
		Name:        "Should Not Change",
		Address:     "Should Not Change",
	}

	// 使用 OnConflict，但 DoUpdates 只包含主键列
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "APPLY_NUMBER"}, Value: conflictRecord.ApplyNumber}, // 主键，应该被过滤
			{Column: clause.Column{Name: "MERCHANT_ID"}, Value: conflictRecord.MerchantID},   // 主键，应该被过滤
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	// 验证记录没有被更新（因为主键更新被过滤掉了）
	var unchangedRecord MerchantMore
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP002", "MCH002").First(&unchangedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch record: %v", err)
	}

	// 非主键字段应该保持原始值，因为更新操作被过滤了
	if unchangedRecord.Name != "Initial Name" {
		t.Errorf("Expected Name to remain 'Initial Name', got '%s'", unchangedRecord.Name)
	}

	if unchangedRecord.Address != "Initial Address" {
		t.Errorf("Expected Address to remain 'Initial Address', got '%s'", unchangedRecord.Address)
	}
}

func TestOnConflictDoUpdateNonPrimaryKeysOnly(t *testing.T) {
	// 这个测试验证当 DoUpdates 只包含非主键列时，所有更新都应该正常执行
	if err := DB.AutoMigrate(&MerchantMore{}); err != nil {
		t.Skipf("Skipping test due to migration error: %v", err)
	}
	
	clearTable(t, "T_MERCHANT_MORE")

	// 创建初始记录
	initialRecord := MerchantMore{
		ApplyNumber: "APP003",
		MerchantID:  "MCH003",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 准备冲突更新，只更新非主键字段
	conflictRecord := MerchantMore{
		ApplyNumber: "APP003",
		MerchantID:  "MCH003",
		Name:        "New Name",
		Address:     "New Address",
	}

	// 使用 OnConflict，DoUpdates 只包含非主键列
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "NAME"}, Value: conflictRecord.Name},       // 非主键，应该被更新
			{Column: clause.Column{Name: "ADDRESS"}, Value: conflictRecord.Address}, // 非主键，应该被更新
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	// 验证非主键字段被正确更新
	var updatedRecord MerchantMore
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP003", "MCH003").First(&updatedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch updated record: %v", err)
	}

	if updatedRecord.Name != "New Name" {
		t.Errorf("Expected Name to be 'New Name', got '%s'", updatedRecord.Name)
	}

	if updatedRecord.Address != "New Address" {
		t.Errorf("Expected Address to be 'New Address', got '%s'", updatedRecord.Address)
	}

	// 主键应该保持不变
	if updatedRecord.ApplyNumber != "APP003" {
		t.Errorf("Primary key ApplyNumber should remain 'APP003', got '%s'", updatedRecord.ApplyNumber)
	}

	if updatedRecord.MerchantID != "MCH003" {
		t.Errorf("Primary key MerchantID should remain 'MCH003', got '%s'", updatedRecord.MerchantID)
	}
}