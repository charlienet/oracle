package oracle

import (
	"os"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TestBatchMerge_BasicInsert 测试批量插入
func TestBatchMerge_BasicInsert(t *testing.T) {
	type TestUser struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 批量插入
	users := []TestUser{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
	}

	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("Batch insert failed: %v", err)
	}

	// 验证数据
	var count int64
	if err := db.Model(&TestUser{}).Count(&count).Error; err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 records, got %d", count)
	}
}

// TestBatchMerge_Upsert 测试批量 UPSERT
func TestBatchMerge_Upsert(t *testing.T) {
	type TestUser struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 先插入一些数据
	db.Create(&TestUser{ID: 1, Name: "Alice Old"})
	db.Create(&TestUser{ID: 2, Name: "Bob Old"})

	// 批量 UPSERT
	users := []TestUser{
		{ID: 1, Name: "Alice New"}, // 更新
		{ID: 2, Name: "Bob New"},   // 更新
		{ID: 3, Name: "Charlie"},   // 插入
	}

	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&users).Error

	if err != nil {
		t.Fatalf("Batch UPSERT failed: %v", err)
	}

	// 验证数据
	var user1, user2, user3 TestUser
	db.First(&user1, 1)
	db.First(&user2, 2)
	db.First(&user3, 3)

	if user1.Name != "Alice New" {
		t.Errorf("Expected 'Alice New', got '%s'", user1.Name)
	}
	if user2.Name != "Bob New" {
		t.Errorf("Expected 'Bob New', got '%s'", user2.Name)
	}
	if user3.Name != "Charlie" {
		t.Errorf("Expected 'Charlie', got '%s'", user3.Name)
	}
}

// TestBatchMerge_LargeDataset 测试大数据量
func TestBatchMerge_LargeDataset(t *testing.T) {
	type TestUser struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 批量插入 100 条数据
	users := make([]TestUser, 100)
	for i := 0; i < 100; i++ {
		users[i] = TestUser{
			ID:   uint(i + 1),
			Name: "User" + string(rune('A'+i%26)),
		}
	}

	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("Large batch insert failed: %v", err)
	}

	// 验证数据
	var count int64
	if err := db.Model(&TestUser{}).Count(&count).Error; err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 100 {
		t.Errorf("Expected 100 records, got %d", count)
	}
}

// TestBatchMerge_WithAutoIncrement 测试带自增主键的批量插入
func TestBatchMerge_WithAutoIncrement(t *testing.T) {
	type TestUser struct {
		ID   uint   `gorm:"primaryKey;autoIncrement"`
		Name string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 批量插入（不指定 ID）
	users := []TestUser{
		{Name: "Alice"},
		{Name: "Bob"},
		{Name: "Charlie"},
	}

	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("Batch insert with auto-increment failed: %v", err)
	}

	// 验证 ID 已回填
	for i, user := range users {
		if user.ID == 0 {
			t.Errorf("User %d: ID not filled back", i)
		}
	}
}

// TestBatchMerge_EmptySlice 测试空数据
func TestBatchMerge_EmptySlice(t *testing.T) {
	type TestUser struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	// 批量插入空数据
	users := []TestUser{}

	err := db.Create(&users).Error
	if err == nil {
		t.Error("Expected error for empty slice, got nil")
	}
}

// TestBatchMerge_WithNullValues 测试 NULL 值
func TestBatchMerge_WithNullValues(t *testing.T) {
	type TestUser struct {
		ID    uint    `gorm:"primaryKey"`
		Name  string  `gorm:"size:100"`
		Email *string `gorm:"size:100"`
	}

	db := setupTestDB(t)
	defer cleanupTestDB(t, db)

	// 创建表
	if err := db.AutoMigrate(&TestUser{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	email := "alice@example.com"
	users := []TestUser{
		{ID: 1, Name: "Alice", Email: &email},
		{ID: 2, Name: "Bob", Email: nil}, // NULL
	}

	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("Batch insert with NULL failed: %v", err)
	}

	// 验证数据
	var user1, user2 TestUser
	db.First(&user1, 1)
	db.First(&user2, 2)

	if user1.Email == nil || *user1.Email != email {
		t.Errorf("User 1: Email not correct")
	}
	if user2.Email != nil {
		t.Errorf("User 2: Email should be NULL")
	}
}

// setupTestDB 创建测试数据库连接
// 优先读取环境变量 ORACLE_DSN；未设置时跳过（与 tests/main_test.go 的做法一致）。
// 注：原先这里硬编码的开发用占位 DSN（oracle://testuser:testpass@localhost:1521/jempdb）
// 已移除，避免测试凭据写入源码。
func setupTestDB(t *testing.T) *gorm.DB {
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN 未设置，跳过真实库测试")
	}
	db, err := gorm.Open(Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	return db
}

// cleanupTestDB 清理测试数据
func cleanupTestDB(t *testing.T, db *gorm.DB) {
	// 删除所有测试表
	tables := []string{"TEST_USERS"}
	for _, table := range tables {
		db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS")
	}
}
