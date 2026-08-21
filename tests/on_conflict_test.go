package tests

import (
	"gorm.io/gorm/clause"
	"testing"
)

// OnConflictTestModel 是 OnConflict 测试专用模型，使用独立表避免与生产表冲突
type OnConflictTestModel struct {
	ApplyNumber string `gorm:"column:APPLY_NUMBER;primaryKey;size:50"`
	MerchantID  string `gorm:"column:MERCHANT_ID;primaryKey;size:50"`
	Name        string `gorm:"column:NAME;size:100"`
	Address     string `gorm:"column:ADDRESS;size:200"`
}

func (OnConflictTestModel) TableName() string {
	return "TEST_ON_CONFLICT"
}

func setupOnConflictTest(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&OnConflictTestModel{}); err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}
	clearTable(t, "TEST_ON_CONFLICT")
}

func TestOnConflictDoUpdateWithPrimaryKeyFilter(t *testing.T) {
	setupOnConflictTest(t)

	// 创建初始记录
	initialRecord := OnConflictTestModel{
		ApplyNumber: "APP001",
		MerchantID:  "MCH001",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 准备冲突更新，尝试更新包括主键在内的字段
	conflictRecord := OnConflictTestModel{
		ApplyNumber: "APP001",
		MerchantID:  "MCH001",
		Name:        "Updated Name",
		Address:     "Updated Address",
	}

	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "APPLY_NUMBER"}, Value: conflictRecord.ApplyNumber},
			{Column: clause.Column{Name: "MERCHANT_ID"}, Value: conflictRecord.MerchantID},
			{Column: clause.Column{Name: "NAME"}, Value: conflictRecord.Name},
			{Column: clause.Column{Name: "ADDRESS"}, Value: conflictRecord.Address},
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	var updatedRecord OnConflictTestModel
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP001", "MCH001").First(&updatedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch updated record: %v", err)
	}

	if updatedRecord.Name != "Updated Name" {
		t.Errorf("Expected Name to be 'Updated Name', got '%s'", updatedRecord.Name)
	}
	if updatedRecord.Address != "Updated Address" {
		t.Errorf("Expected Address to be 'Updated Address', got '%s'", updatedRecord.Address)
	}
	if updatedRecord.ApplyNumber != "APP001" {
		t.Errorf("Primary key ApplyNumber should remain 'APP001', got '%s'", updatedRecord.ApplyNumber)
	}
	if updatedRecord.MerchantID != "MCH001" {
		t.Errorf("Primary key MerchantID should remain 'MCH001', got '%s'", updatedRecord.MerchantID)
	}
}

func TestOnConflictDoUpdateOnlyPrimaryKeys(t *testing.T) {
	setupOnConflictTest(t)

	initialRecord := OnConflictTestModel{
		ApplyNumber: "APP002",
		MerchantID:  "MCH002",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	conflictRecord := OnConflictTestModel{
		ApplyNumber: "APP002",
		MerchantID:  "MCH002",
		Name:        "Should Not Change",
		Address:     "Should Not Change",
	}

	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "APPLY_NUMBER"}, Value: conflictRecord.ApplyNumber},
			{Column: clause.Column{Name: "MERCHANT_ID"}, Value: conflictRecord.MerchantID},
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	var unchangedRecord OnConflictTestModel
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP002", "MCH002").First(&unchangedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch record: %v", err)
	}

	if unchangedRecord.Name != "Initial Name" {
		t.Errorf("Expected Name to remain 'Initial Name', got '%s'", unchangedRecord.Name)
	}
	if unchangedRecord.Address != "Initial Address" {
		t.Errorf("Expected Address to remain 'Initial Address', got '%s'", unchangedRecord.Address)
	}
}

func TestOnConflictDoUpdateNonPrimaryKeysOnly(t *testing.T) {
	setupOnConflictTest(t)

	initialRecord := OnConflictTestModel{
		ApplyNumber: "APP003",
		MerchantID:  "MCH003",
		Name:        "Initial Name",
		Address:     "Initial Address",
	}
	if err := DB.Create(&initialRecord).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	conflictRecord := OnConflictTestModel{
		ApplyNumber: "APP003",
		MerchantID:  "MCH003",
		Name:        "New Name",
		Address:     "New Address",
	}

	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "NAME"}, Value: conflictRecord.Name},
			{Column: clause.Column{Name: "ADDRESS"}, Value: conflictRecord.Address},
		},
	}).Create(&conflictRecord)

	if result.Error != nil {
		t.Fatalf("OnConflict create failed: %v", result.Error)
	}

	var updatedRecord OnConflictTestModel
	if err := DB.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "APP003", "MCH003").First(&updatedRecord).Error; err != nil {
		t.Fatalf("Failed to fetch updated record: %v", err)
	}

	if updatedRecord.Name != "New Name" {
		t.Errorf("Expected Name to be 'New Name', got '%s'", updatedRecord.Name)
	}
	if updatedRecord.Address != "New Address" {
		t.Errorf("Expected Address to be 'New Address', got '%s'", updatedRecord.Address)
	}
	if updatedRecord.ApplyNumber != "APP003" {
		t.Errorf("Primary key ApplyNumber should remain 'APP003', got '%s'", updatedRecord.ApplyNumber)
	}
	if updatedRecord.MerchantID != "MCH003" {
		t.Errorf("Primary key MerchantID should remain 'MCH003', got '%s'", updatedRecord.MerchantID)
	}
}