package tests

import (
	"fmt"
	"log"
	"os"
	"testing"

	"gorm.io/gorm"

	oracle "github.com/charlienet/go-oracle"
)

var DB *gorm.DB

func TestMain(m *testing.M) {
	// 使用提供的 DSN（可通过 ORACLE_DSN 环境变量覆盖）
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		// 无数据库环境：跳过集成测试，避免使用占位 DSN 强连后整包崩溃
		fmt.Fprintln(os.Stderr, "未设置 ORACLE_DSN，跳过集成测试")
		os.Exit(0)
	}

	var err error
	DB, err = gorm.Open(oracle.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	log.Println("successfully connected to database")

	// 清理测试表
	cleanup()

	// 运行测试
	code := m.Run()

	// 最终清理
	cleanup()

	os.Exit(code)
}

// cleanup 删除所有测试表（忽略错误，因为表可能不存在）
func cleanup() {
	// 清理顺序：先删除子表（有外键约束），再删除主表
	// 使用原生 SQL 确保表名正确（Oracle 默认大写）

	// 1. 删除子表（依赖 User 的表）
	DB.Exec("DROP TABLE TEST_ORDERS")   // 外键 UserID -> TEST_USERS
	DB.Exec("DROP TABLE TEST_PROFILES") // 外键 UserID -> TEST_USERS
	DB.Exec("DROP TABLE USER_ROLES")    // Many2Many 关联表

	// 2. 删除其他独立表
	DB.Exec("DROP TABLE TEST_ROLES") // 被 USER_ROLES 引用
	DB.Exec("DROP TABLE TEST_USERS") // User 主表
	DB.Exec("DROP TABLE TEST_PRODUCTS")
	DB.Exec("DROP TABLE TEST_MERCHANTS")
	DB.Exec("DROP TABLE TEST_ENUM_MERCHANTS")
	DB.Exec("DROP TABLE TEST_ENUM_STRING_MERCHANTS")
	DB.Exec("DROP TABLE TEST_SEQ_DEF")     // SeqDefaultViaDriverModel 表
	DB.Exec("DROP TABLE TEST_SEQ_DEFAULT") // 原生 SQL 创建的序列默认值表
	DB.Exec("DROP TABLE TEST_USERS_HOOK")  // UserWithHook 表
	DB.Exec("DROP TABLE TEST_BIG_STRING")  // BigStringModel 表

	// 3. 清理序列
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEFAULT")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF_CODE")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF")
}

// clearTable 清空指定测试表，保证测试之间的数据隔离
func clearTable(t *testing.T, table string) {
	t.Helper()
	if err := DB.Exec("DELETE FROM " + table).Error; err != nil {
		t.Fatalf("failed to clear table %s: %v", table, err)
	}
}

// clearUserTables 清空所有与 User 相关的表（按正确顺序）
func clearUserTables(t *testing.T) {
	t.Helper()
	// 先清理子表（依赖 User 的表）
	// 忽略表不存在的错误，确保清理继续
	DB.Exec("DELETE FROM TEST_ORDERS")
	DB.Exec("DELETE FROM TEST_PROFILES")
	// 清理关联表
	DB.Exec("DELETE FROM USER_ROLES")
	DB.Exec("DELETE FROM TEST_ROLES")
	// 最后清理 User 主表
	DB.Exec("DELETE FROM TEST_USERS")
}
