package tests

import (
	"testing"
)

func TestQuerySingle(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 创建测试数据
	user := User{Name: "Query Test", Email: "query@example.com", Age: 35}
	DB.Create(&user)

	// 查询
	var found User
	result := DB.First(&found, user.ID)
	if result.Error != nil {
		t.Fatalf("failed to query: %v", result.Error)
	}

	if found.Name != "Query Test" {
		t.Errorf("expected name 'Query Test', got '%s'", found.Name)
	}
}

func TestQueryWithConditions(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 创建测试数据
	users := []User{
		{Name: "Condition 1", Email: "cond1@example.com", Age: 20, Active: true},
		{Name: "Condition 2", Email: "cond2@example.com", Age: 25, Active: true},
		{Name: "Condition 3", Email: "cond3@example.com", Age: 30, Active: false},
	}
	DB.Create(&users)

	// 条件查询
	var results []User
	result := DB.Where("active = ? AND age > ?", true, 22).Find(&results)
	if result.Error != nil {
		t.Fatalf("failed to query: %v", result.Error)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestQueryWithLimit(t *testing.T) {
	// 先确保表存在并清空
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 创建测试数据
	for i := range 10 {
		user := User{
			Name:  "Limit Test",
			Email: "limit" + string(rune('a'+i)) + "@example.com",
			Age:   i,
		}
		DB.Create(&user)
	}

	// 分页查询
	var results []User
	result := DB.Limit(5).Offset(2).Find(&results)
	if result.Error != nil {
		t.Fatalf("failed to query: %v", result.Error)
	}

	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}
