package tests

import (
	"strconv"
	"strings"
	"testing"

	oracle "github.com/charlienet/oracle"
)

// TestGetTables 验证 GetTables 返回当前用户的表列表（Oracle 数据字典 USER_TABLES）
func TestGetTables(t *testing.T) {
	// 1. 创建测试表
	type TestGetTablesA struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"size:50"`
	}
	type TestGetTablesB struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Code string `gorm:"size:20"`
	}

	if err := DB.AutoMigrate(&TestGetTablesA{}, &TestGetTablesB{}); err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestGetTablesA{}, &TestGetTablesB{})
	}()

	// 2. 调用 GetTables
	tables, err := DB.Migrator().GetTables()
	if err != nil {
		t.Fatalf("GetTables returned error: %v", err)
	}

	// 3. 断言返回列表包含测试表（大小写不敏感比较，Oracle 默认大写）
	var foundA, foundB bool
	for _, table := range tables {
		upper := strings.ToUpper(table)
		if upper == "TEST_GET_TABLES_AS" {
			foundA = true
		}
		if upper == "TEST_GET_TABLES_BS" {
			foundB = true
		}
	}

	if !foundA {
		t.Error("expected TEST_GET_TABLES_AS in table list")
	}
	if !foundB {
		t.Error("expected TEST_GET_TABLES_BS in table list")
	}
}

// TestDropView 验证 DropView 能正确删除视图（包括不存在的视图不报错，幂等性）
func TestDropView(t *testing.T) {
	viewName := "TEST_DROP_VIEW"

	// 1. 创建测试视图
	createViewSQL := "CREATE OR REPLACE VIEW " + viewName + " AS SELECT 1 AS ID FROM DUAL"
	if err := DB.Exec(createViewSQL).Error; err != nil {
		t.Fatalf("failed to create test view: %v", err)
	}

	// 清理：确保测试结束时删除视图
	defer func() {
		_ = DB.Exec("DROP VIEW " + viewName)
	}()

	// 2. 调用 DropView（视图存在）
	if err := DB.Migrator().DropView(viewName); err != nil {
		t.Errorf("DropView(existing view) returned error: %v", err)
	}

	// 3. 再次调用 DropView（视图已不存在）- 验证幂等性
	if err := DB.Migrator().DropView(viewName); err != nil {
		t.Errorf("DropView(non-existent view) should not error (idempotent), got: %v", err)
	}
}

// TestGetIndexes 验证 GetIndexes 返回表的索引列表
func TestGetIndexes(t *testing.T) {
	// 1. 创建测试表 + 索引
	type TestGetIndexesUser struct {
		ID    uint   `gorm:"column:id;primaryKey"`
		Email string `gorm:"column:email;size:100;uniqueIndex:idx_email"`
		Name  string `gorm:"column:name;size:50"`
	}

	if err := DB.AutoMigrate(&TestGetIndexesUser{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestGetIndexesUser{})
	}()

	// 2. 调用 DB.Migrator().GetIndexes("TEST_GET_INDEXES_USERS")
	indexes, err := DB.Migrator().GetIndexes(&TestGetIndexesUser{})
	if err != nil {
		t.Fatalf("GetIndexes returned error: %v", err)
	}

	// 3. 断言返回列表包含索引名（大小写不敏感）
	var foundEmailIdx bool
	for _, idx := range indexes {
		upper := strings.ToUpper(idx.Name())
		if strings.Contains(upper, "EMAIL") {
			foundEmailIdx = true
		}
	}

	if !foundEmailIdx {
		t.Errorf("expected index with 'EMAIL' in name, got %v", indexes)
	}
}

// TestTableType 验证 TableType 返回表类型
func TestTableType(t *testing.T) {
	// 1. 创建测试表
	type TestTableTypeModel struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"column:name;size:50"`
	}

	if err := DB.AutoMigrate(&TestTableTypeModel{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestTableTypeModel{})
	}()

	// 2. 调用 DB.Migrator().TableType("TEST_TABLE_TYPE_MODELS")
	tableType, err := DB.Migrator().TableType(&TestTableTypeModel{})
	if err != nil {
		t.Fatalf("TableType returned error: %v", err)
	}

	// 3. 断言返回 "TABLE"（大小写不敏感）
	if strings.ToUpper(tableType.Type()) != "TABLE" {
		t.Errorf("expected table type 'TABLE', got %q", tableType.Type())
	}
}

// getDBVersion 获取数据库版本的主版本号
func getDBVersion() int {
	d, ok := DB.Dialector.(*oracle.Dialector)
	if !ok || d.DBVer == "" {
		return 0
	}
	major, _ := strconv.Atoi(strings.Split(d.DBVer, ".")[0])
	return major
}

// TestRenameColumn_12c 验证 12c+ 支持 RENAME COLUMN
func TestRenameColumn_12c(t *testing.T) {
	// 1. 检查版本（跳过非 12c+）
	dbVer := getDBVersion()
	if dbVer < 12 {
		t.Skip("Skipping test for Oracle 11g (requires 12c+)")
	}

	// 2. 创建测试表
	type TestRenameColumn12cModel struct {
		ID        uint   `gorm:"column:id;primaryKey"`
		OldColumn string `gorm:"column:old_column;size:50"`
	}

	if err := DB.AutoMigrate(&TestRenameColumn12cModel{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestRenameColumn12cModel{})
	}()

	// 3. 调用 DB.Migrator().RenameColumn
	err := DB.Migrator().RenameColumn(&TestRenameColumn12cModel{}, "old_column", "new_column")
	if err != nil {
		t.Fatalf("RenameColumn returned error: %v", err)
	}

	// 4. 断言列已重命名（检查新列是否存在）
	if !DB.Migrator().HasColumn(&TestRenameColumn12cModel{}, "new_column") {
		t.Error("expected new_column to exist after rename")
	}
}

// TestRenameColumn_11g 验证 11g 返回错误
func TestRenameColumn_11g(t *testing.T) {
	// 1. 检查版本（跳过非 11g）
	dbVer := getDBVersion()
	if dbVer >= 12 {
		t.Skip("Skipping test for Oracle 12c+ (only for 11g)")
	}

	// 2. 创建测试表
	type TestRenameColumn11gModel struct {
		ID        uint   `gorm:"column:id;primaryKey"`
		OldColumn string `gorm:"column:old_column;size:50"`
	}

	if err := DB.AutoMigrate(&TestRenameColumn11gModel{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestRenameColumn11gModel{})
	}()

	// 3. 调用 DB.Migrator().RenameColumn
	err := DB.Migrator().RenameColumn(&TestRenameColumn11gModel{}, "old_column", "new_column")

	// 4. 断言返回错误（不支持）
	if err == nil {
		t.Fatal("expected error for RENAME COLUMN on Oracle 11g, got nil")
	}

	// 验证错误信息包含版本说明
	if !strings.Contains(err.Error(), "11g") && !strings.Contains(err.Error(), "不支持") {
		t.Errorf("expected error message to mention Oracle 11g or '不支持', got: %v", err)
	}
}

// TestMigrateColumn_TypeChange 验证列类型变更使用 MODIFY 语法
func TestMigrateColumn_TypeChange(t *testing.T) {
	// 1. 创建测试表（varchar(50)）
	type TestMigrateColumnV1 struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"column:name;type:varchar(50)"`
	}

	if err := DB.AutoMigrate(&TestMigrateColumnV1{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	// 2. 修改模型（varchar(100)）
	type TestMigrateColumnV2 struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"column:name;type:varchar(100)"`
	}

	// 3. 再次 AutoMigrate（触发 MigrateColumn）
	if err := DB.AutoMigrate(&TestMigrateColumnV2{}); err != nil {
		t.Fatalf("failed to migrate column: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestMigrateColumnV1{})
	}()

	// 4. 查询列类型，断言已变为 varchar(100)
	columnTypes, err := DB.Migrator().ColumnTypes(&TestMigrateColumnV2{})
	if err != nil {
		t.Fatalf("failed to get column types: %v", err)
	}

	for _, ct := range columnTypes {
		if strings.ToUpper(ct.Name()) == "NAME" {
			dataType := strings.ToLower(ct.DatabaseTypeName())
			// Oracle 报告类型为 varchar2，需要检查长度
			length, ok := ct.Length()
			if !ok {
				t.Errorf("failed to get length for column 'name'")
				return
			}

			// 验证长度已从 50 变为 100
			if length != 100 {
				t.Errorf("expected column 'name' length to be 100, got %d", length)
			}

			t.Logf("Column 'name' type: %s, length: %d", dataType, length)
			return
		}
	}

	t.Error("column 'name' not found")
}

// TestMigrateColumn_NullableChange 验证可空性变更
func TestMigrateColumn_NullableChange(t *testing.T) {
	// 1. 创建测试表（not null）
	type TestMigrateColumnNotNull struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"column:name;not null"`
	}

	if err := DB.AutoMigrate(&TestMigrateColumnNotNull{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}

	// 2. 修改模型（可空）
	type TestMigrateColumnNullable struct {
		ID   uint   `gorm:"column:id;primaryKey"`
		Name string `gorm:"column:name"`
	}

	// 3. 再次 AutoMigrate（触发 MigrateColumn）
	if err := DB.AutoMigrate(&TestMigrateColumnNullable{}); err != nil {
		t.Fatalf("failed to migrate column: %v", err)
	}
	defer func() {
		_ = DB.Migrator().DropTable(&TestMigrateColumnNotNull{})
	}()

	// 4. 查询列可空性，断言已变为可空
	columnTypes, err := DB.Migrator().ColumnTypes(&TestMigrateColumnNullable{})
	if err != nil {
		t.Fatalf("failed to get column types: %v", err)
	}

	for _, ct := range columnTypes {
		if strings.ToUpper(ct.Name()) == "NAME" {
			nullable, ok := ct.Nullable()
			if !ok {
				t.Errorf("failed to get nullable for column 'name'")
				return
			}

			// 验证列已变为可空
			if !nullable {
				t.Errorf("expected column 'name' to be nullable, got not null")
			}

			t.Logf("Column 'name' nullable: %v", nullable)
			return
		}
	}

	t.Error("column 'name' not found")
}
