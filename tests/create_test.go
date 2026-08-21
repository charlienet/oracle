package tests

import (
	"testing"
	"time"
)

func TestCreateSingle(t *testing.T) {
	// 先创建表
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	user := User{
		Name:   "Test User",
		Email:  "test@example.com",
		Age:    25,
		Active: true,
	}

	result := DB.Create(&user)
	if result.Error != nil {
		t.Fatalf("failed to create user: %v", result.Error)
	}

	if user.ID == 0 {
		t.Error("expected user ID to be set after create")
	}

	t.Logf("Created user with ID: %d", user.ID)
}

func TestCreateBatch(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	users := []User{
		{Name: "User 1", Email: "user1@example.com", Age: 20},
		{Name: "User 2", Email: "user2@example.com", Age: 22},
		{Name: "User 3", Email: "user3@example.com", Age: 24},
	}

	result := DB.Create(&users)
	if result.Error != nil {
		t.Fatalf("failed to create users: %v", result.Error)
	}

	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}

	for i, user := range users {
		if user.ID == 0 {
			t.Errorf("user %d: expected ID to be set", i)
		}
	}
}

func TestCreateWithTimestamp(t *testing.T) {
	if err := DB.AutoMigrate(&Product{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_PRODUCTS")

	product := Product{
		Name:        "Test Product",
		Price:       99.99,
		Stock:       100,
		Description: "This is a test product with long description",
		CreatedAt:   time.Now(),
	}

	result := DB.Create(&product)
	if result.Error != nil {
		t.Fatalf("failed to create product: %v", result.Error)
	}

	if product.ID == 0 {
		t.Error("expected product ID to be set")
	}
}

func TestBatchInsertWithReturning(t *testing.T) {
	// 测试批量插入 + RETURNING INTO 场景
	// 验证 Vars 不会覆盖输出参数
	
	// 1. 创建测试表（带自增主键）
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 2. 批量插入多条记录
	users := []User{
		{Name: "Batch User 1", Email: "batch1@example.com", Age: 25},
		{Name: "Batch User 2", Email: "batch2@example.com", Age: 30},
		{Name: "Batch User 3", Email: "batch3@example.com", Age: 35},
	}

	result := DB.Create(&users)
	if result.Error != nil {
		t.Fatalf("failed to create users: %v", result.Error)
	}

	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}

	// 3. 验证主键回填正确
	for i, user := range users {
		if user.ID == 0 {
			t.Errorf("user %d: expected ID to be set after batch insert", i)
		}
		t.Logf("User %d created with ID: %d", i+1, user.ID)
	}

	// 4. 验证数据正确插入
	var insertedUsers []User
	if err := DB.Find(&insertedUsers).Error; err != nil {
		t.Fatalf("failed to query users: %v", err)
	}

	if len(insertedUsers) != 3 {
		t.Errorf("expected 3 users in database, got %d", len(insertedUsers))
	}

	for i, user := range users {
		found := false
		for _, inserted := range insertedUsers {
			if inserted.ID == user.ID && inserted.Name == user.Name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("user %d (ID: %d) not found in database", i, user.ID)
		}
	}
}
