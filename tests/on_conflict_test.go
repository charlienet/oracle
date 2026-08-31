package tests

import (
	"strconv"
	"strings"
	"testing"

	oracle "github.com/charlienet/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// PKOnlyModel 用于测试只有主键字段的极端情况
type PKOnlyModel struct {
	ID uint `gorm:"column:ID;primaryKey"`
}

func (PKOnlyModel) TableName() string {
	return "TEST_PK_ONLY"
}

// NormalModelForMerge 用于测试主键 + 普通字段的正常情况
type NormalModelForMerge struct {
	ID   uint   `gorm:"column:ID;primaryKey"`
	Name string `gorm:"column:NAME;size:100"`
}

func (NormalModelForMerge) TableName() string {
	return "TEST_NORMAL_MERGE"
}

// TestMergeUpdateAllWithPrimaryKeyOnly 验证 MERGE + UpdateAll=true 时，
// 如果模型只有主键字段，不会抛出错误（预期：DoNothing 或 INSERT 成功）
func TestMergeUpdateAllWithPrimaryKeyOnly(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&PKOnlyModel{}); err != nil {
		t.Fatalf("Failed to migrate PKOnlyModel: %v", err)
	}
	clearTable(t, "TEST_PK_ONLY")

	// 1. 创建初始记录（ID=1）
	initial := PKOnlyModel{ID: 1}
	if err := DB.Create(&initial).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 2. 使用 UpdateAll=true 再次创建（相同 ID）
	// 预期：DoNothing 或静默失败（不报错）
	conflict := PKOnlyModel{ID: 1}
	result := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&conflict)

	// 断言：不报错
	if result.Error != nil {
		t.Errorf("Expected no error for UpdateAll with PK-only model, got: %v", result.Error)
	}

	// 3. 查询记录，断言存在（INSERT 或 DoNothing 都应保留记录）
	var found PKOnlyModel
	if err := DB.Where("ID = ?", 1).First(&found).Error; err != nil {
		t.Errorf("Record should exist after UpdateAll, but got error: %v", err)
	}
}

// TestMergeUpdateAllWithNonPrimaryKey 验证 MERGE + UpdateAll=true 正常场景
func TestMergeUpdateAllWithNonPrimaryKey(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&NormalModelForMerge{}); err != nil {
		t.Fatalf("Failed to migrate NormalModelForMerge: %v", err)
	}
	clearTable(t, "TEST_NORMAL_MERGE")

	// 1. 创建初始记录
	initial := NormalModelForMerge{ID: 1, Name: "original"}
	if err := DB.Create(&initial).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 2. 使用 UpdateAll=true 创建（相同 ID，修改 Name）
	conflict := NormalModelForMerge{ID: 1, Name: "updated"}
	result := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&conflict)

	// 断言：不报错
	if result.Error != nil {
		t.Fatalf("Expected no error for UpdateAll, got: %v", result.Error)
	}

	// 3. 查询记录，断言 Name 已更新
	var found NormalModelForMerge
	if err := DB.Where("ID = ?", 1).First(&found).Error; err != nil {
		t.Fatalf("Record should exist after UpdateAll, but got error: %v", err)
	}

	if found.Name != "updated" {
		t.Errorf("Expected Name to be 'updated', got '%s'", found.Name)
	}
}

// TestFullSaveAssociations 验证 FullSaveAssociations 模式下的 MERGE 行为
func TestFullSaveAssociations(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&User{}, &Profile{}); err != nil {
		t.Fatalf("Failed to migrate User and Profile: %v", err)
	}
	clearUserTables(t)
	clearTable(t, "TEST_PROFILES")

	// 1. 创建初始 User 和 Profile（手动创建，避免 autoIncrement）
	user := User{
		Name:  "Alice",
		Email: "alice@example.com",
		Profile: Profile{
			Bio: "original bio",
		},
	}
	if err := DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create initial user: %v", err)
	}

	// 2. 使用 Session{FullSaveAssociations: true} 更新 User（修改 Profile.Bio）
	updatedUser := User{
		ID:    user.ID,
		Name:  "Alice Updated",
		Email: "alice.updated@example.com",
		Profile: Profile{
			ID:     user.Profile.ID,
			UserID: user.ID,
			Bio:    "updated bio",
		},
	}
	result := DB.Session(&gorm.Session{FullSaveAssociations: true}).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&updatedUser)

	// 断言：不报错
	if result.Error != nil {
		t.Fatalf("Expected no error for FullSaveAssociations, got: %v", result.Error)
	}

	// 3. 查询 Profile，断言 Bio 已更新
	var foundProfile Profile
	if err := DB.Where("id = ?", user.Profile.ID).First(&foundProfile).Error; err != nil {
		t.Fatalf("Profile should exist after FullSaveAssociations, but got error: %v", err)
	}

	if foundProfile.Bio != "updated bio" {
		t.Errorf("Expected Profile.Bio to be 'updated bio', got '%s'", foundProfile.Bio)
	}
}

// TestMergeDoUpdatesWithOnlyPrimaryKeys 验证显式设置 DoUpdates 只包含主键时的行为
func TestMergeDoUpdatesWithOnlyPrimaryKeys(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&NormalModelForMerge{}); err != nil {
		t.Fatalf("Failed to migrate NormalModelForMerge: %v", err)
	}
	clearTable(t, "TEST_NORMAL_MERGE")

	// 1. 创建初始记录
	initial := NormalModelForMerge{ID: 1, Name: "original"}
	if err := DB.Create(&initial).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 2. 显式设置 DoUpdates 只包含主键（不推荐，但应该不报错）
	conflict := NormalModelForMerge{ID: 1, Name: "should not change"}
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ID"}},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "ID"}, Value: 1},
		},
	}).Create(&conflict)

	// 断言：不报错（filteredSet 为空，不生成 WHEN MATCHED）
	if result.Error != nil {
		t.Fatalf("Expected no error when DoUpdates contains only primary keys, got: %v", result.Error)
	}

	// 3. 查询记录，断言 Name 未改变（没有更新）
	var found NormalModelForMerge
	if err := DB.Where("ID = ?", 1).First(&found).Error; err != nil {
		t.Fatalf("Record should exist, but got error: %v", err)
	}

	if found.Name != "original" {
		t.Errorf("Name should remain 'original' (DoNothing), got '%s'", found.Name)
	}
}

// TestMergeDoUpdatesWithMixedKeys 验证 DoUpdates 包含主键和非主键时的行为
func TestMergeDoUpdatesWithMixedKeys(t *testing.T) {
	t.Helper()
	if err := DB.AutoMigrate(&NormalModelForMerge{}); err != nil {
		t.Fatalf("Failed to migrate NormalModelForMerge: %v", err)
	}
	clearTable(t, "TEST_NORMAL_MERGE")

	// 1. 创建初始记录
	initial := NormalModelForMerge{ID: 1, Name: "original"}
	if err := DB.Create(&initial).Error; err != nil {
		t.Fatalf("Failed to create initial record: %v", err)
	}

	// 2. DoUpdates 包含主键和非主键（主键会被过滤，只更新非主键）
	conflict := NormalModelForMerge{ID: 1, Name: "updated"}
	result := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ID"}},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "ID"}, Value: 1},           // 主键，会被过滤
			{Column: clause.Column{Name: "NAME"}, Value: "updated"}, // 非主键，会被保留
		},
	}).Create(&conflict)

	// 断言：不报错
	if result.Error != nil {
		t.Fatalf("Expected no error, got: %v", result.Error)
	}

	// 3. 查询记录，断言 Name 已更新
	var found NormalModelForMerge
	if err := DB.Where("ID = ?", 1).First(&found).Error; err != nil {
		t.Fatalf("Record should exist, but got error: %v", err)
	}

	if found.Name != "updated" {
		t.Errorf("Expected Name to be 'updated', got '%s'", found.Name)
	}
}

// TestMergeReturning_12c 验证 12c+ MERGE 是否支持 RETURNING 回填默认值字段。
// 实测结论（Oracle 12.2.0.1, JEMPDB 容器）：Oracle 的 MERGE 语句不支持
// RETURNING 子句——在 WHEN MATCHED THEN UPDATE SET 之后与 WHEN NOT MATCHED
// THEN INSERT 之后两个分支位置均报 ORA-00933: SQL command not properly ended；
// 对照组 UPDATE ... RETURNING INTO 绑定正常（err=nil）、INSERT ... RETURNING
// 语法通过。故 supportsMergeReturning 恒 false，MERGE 分支不输出 RETURNING，
// 默认值字段（如 CREATED_BY）在 MERGE 分支无法回填，属 Oracle 语法限制。
func TestMergeReturning_12c(t *testing.T) {
	t.Helper()

	// 检查版本，跳过 11g
	d, ok := DB.Dialector.(*oracle.Dialector)
	if !ok {
		t.Fatalf("Dialector 类型断言失败: %T", DB.Dialector)
	}

	major, err := strconv.Atoi(strings.Split(d.DBVer, ".")[0])
	if err != nil {
		t.Skipf("无法解析数据库版本: %v", err)
	}

	if major < 12 {
		t.Skipf("Oracle 11g 不支持 MERGE RETURNING，跳过测试（当前版本: %s）", d.DBVer)
	}

	// 实测（12.2.0.1）：MERGE 语句在任一分支位置带 RETURNING 均报 ORA-00933，
	// Oracle MERGE 不支持 RETURNING（与版本无关的语法限制），本实现不输出
	// RETURNING，默认值字段无法回填。待确认未来 Oracle 版本支持后再启用断言。
	t.Skipf("Oracle %d MERGE 不支持 RETURNING（实测任一分支位置均 ORA-00933），跳过", major)
}
