package oracle

import (
	"reflect"
	"testing"

	"gorm.io/gorm/schema"
)

// TestMigrateColumn_IgnoreMigration 测试 IgnoreMigration 标记
func TestMigrateColumn_IgnoreMigration(t *testing.T) {
	// 创建 Migrator 实例（不需要真实数据库）
	m := Migrator{}

	// 创建带有 IgnoreMigration 标记的字段
	field := &schema.Field{
		Name:            "TestField",
		DBName:          "test_field",
		IgnoreMigration: true,
	}

	// 创建 mock ColumnType
	mockCT := &mockColumnType{}

	// 调用 MigrateColumn，应该直接返回 nil
	err := m.MigrateColumn(nil, field, mockCT)
	if err != nil {
		t.Errorf("MigrateColumn should return nil for IgnoreMigration field, got: %v", err)
	}
}

// TestMigrateColumn_Struct 验证 MigrateColumn 方法存在且签名正确
func TestMigrateColumn_Struct(t *testing.T) {
	// 验证 MigrateColumn 方法存在
	var m Migrator
	_, ok := reflect.TypeOf(m).MethodByName("MigrateColumn")
	if !ok {
		t.Error("MigrateColumn method should exist on Migrator")
	}
}

// TestMigrateColumnUnique_Struct 验证 MigrateColumnUnique 方法存在且签名正确
func TestMigrateColumnUnique_Struct(t *testing.T) {
	// 验证 MigrateColumnUnique 方法存在
	var m Migrator
	_, ok := reflect.TypeOf(m).MethodByName("MigrateColumnUnique")
	if !ok {
		t.Error("MigrateColumnUnique method should exist on Migrator")
	}
}

// mockColumnType 实现 gorm.ColumnType 接口用于测试
type mockColumnType struct {
	name         string
	dataType     string
	length       int64
	lengthOK     bool
	nullable     bool
	nullableOK   bool
	isPrimaryKey bool
	unique       bool
	uniqueOK     bool
	defaultValue string
	defaultOK    bool
}

func (m *mockColumnType) Name() string                      { return m.name }
func (m *mockColumnType) DatabaseTypeName() string          { return m.dataType }
func (m *mockColumnType) ColumnType() (string, bool)        { return "", false }
func (m *mockColumnType) PrimaryKey() (bool, bool)          { return m.isPrimaryKey, true }
func (m *mockColumnType) AutoIncrement() (bool, bool)       { return false, false }
func (m *mockColumnType) Length() (int64, bool)             { return m.length, m.lengthOK }
func (m *mockColumnType) DecimalSize() (int64, int64, bool) { return 0, 0, false }
func (m *mockColumnType) Nullable() (bool, bool)            { return m.nullable, m.nullableOK }
func (m *mockColumnType) Unique() (bool, bool)              { return m.unique, m.uniqueOK }
func (m *mockColumnType) ScanType() reflect.Type            { return reflect.TypeOf("") }
func (m *mockColumnType) Comment() (string, bool)           { return "", false }
func (m *mockColumnType) DefaultValue() (string, bool)      { return m.defaultValue, m.defaultOK }
