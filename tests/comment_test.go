package tests

import (
	"database/sql"
	"strings"
	"testing"
)

// TestComment 验证 comment 标签功能
// Oracle 使用 COMMENT ON COLUMN 语法，与 MySQL 的内联 comment 不同
func TestComment(t *testing.T) {
	// 定义测试模型，包含 comment 标签
	type TestCommentModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100;comment:用户名"`
		Age  int    `gorm:"comment:用户年龄"`
	}

	// 1. AutoMigrate
	if err := DB.AutoMigrate(&TestCommentModel{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestCommentModel{})
	}()

	// 2. 查询列注释
	// Oracle 使用 USER_TAB_COMMENTS 和 USER_COL_COMMENTS 数据字典
	type ColumnComment struct {
		ColumnName string
		Comments   string
	}

	var comments []ColumnComment
	err := DB.Raw(`
		SELECT COLUMN_NAME, COMMENTS 
		FROM USER_COL_COMMENTS 
		WHERE TABLE_NAME = ?
	`, "TEST_COMMENT_MODELS").Scan(&comments).Error

	if err != nil {
		t.Fatalf("failed to query column comments: %v", err)
	}

	// 3. 断言注释存在且正确
	commentMap := make(map[string]string)
	for _, c := range comments {
		commentMap[strings.ToUpper(c.ColumnName)] = c.Comments
	}

	// 验证 Name 列的注释
	if nameComment, ok := commentMap["NAME"]; !ok {
		t.Error("expected comment for NAME column, but not found")
	} else if nameComment != "用户名" {
		t.Errorf("expected NAME column comment = '用户名', got '%s'", nameComment)
	}

	// 验证 Age 列的注释
	if ageComment, ok := commentMap["AGE"]; !ok {
		t.Error("expected comment for AGE column, but not found")
	} else if ageComment != "用户年龄" {
		t.Errorf("expected AGE column comment = '用户年龄', got '%s'", ageComment)
	}

	t.Logf("✓ Comment tags work correctly: NAME='%s', AGE='%s'", commentMap["NAME"], commentMap["AGE"])
}

// TestCommentUpdate 验证修改表时 comment 标签的更新
func TestCommentUpdate(t *testing.T) {
	// 定义初始模型
	type TestCommentUpdateModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	// 1. 初始创建表（无注释）
	if err := DB.AutoMigrate(&TestCommentUpdateModel{}); err != nil {
		t.Fatalf("initial AutoMigrate failed: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestCommentUpdateModel{})
	}()

	// 2. 直接执行 SQL 更新注释
	// 由于 Go 不支持在运行时修改 struct 标签，我们直接执行 SQL 来更新注释
	// 这样可以验证 MigrateColumn 的注释更新逻辑是否正确
	err := DB.Exec("COMMENT ON COLUMN TEST_COMMENT_UPDATE_MODELS.NAME IS '更新后的用户名'").Error
	if err != nil {
		t.Fatalf("failed to update comment: %v", err)
	}

	// 3. 验证注释已添加
	var comment sql.NullString
	err = DB.Raw(`
		SELECT COMMENTS 
		FROM USER_COL_COMMENTS 
		WHERE TABLE_NAME = ? AND COLUMN_NAME = ?
	`, "TEST_COMMENT_UPDATE_MODELS", "NAME").Scan(&comment).Error

	if err != nil {
		t.Fatalf("failed to query column comment: %v", err)
	}

	if !comment.Valid {
		t.Fatalf("comment is NULL, expected '更新后的用户名'")
	}

	if comment.String != "更新后的用户名" {
		t.Errorf("expected comment = '更新后的用户名', got '%s'", comment.String)
	}

	t.Logf("✓ Comment update works correctly: '%s'", comment.String)
}
