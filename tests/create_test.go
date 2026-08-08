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
