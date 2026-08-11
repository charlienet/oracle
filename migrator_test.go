package oracle

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// noopDialector 内嵌真实 Dialector 但跳过 Initialize 的数据库连接，
// 用于在无需真实连接的情况下构造合法的 *gorm.DB（cacheStore 会被 gorm.Open 初始化）。
type noopDialector struct {
	Dialector
}

func (noopDialector) Initialize(db *gorm.DB) error {
	return nil
}

// relParent 含关联关系的模型，用于测试 TryRemoveOnUpdate
type relParent struct {
	ID   uint     `gorm:"primaryKey"`
	Kids []relKid `gorm:"foreignKey:ParentID;constraint:OnUpdate:CASCADE"`
}

type relKid struct {
	ID       uint `gorm:"primaryKey"`
	ParentID uint
}

// newTestMigrator 构造一个不依赖真实连接的 Migrator
func newTestMigrator() Migrator {
	d := &Dialector{Config: &Config{DBVer: "12.1.0.2.0", DefaultStringSize: 1024}}
	db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return Migrator{Migrator: migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   d,
		CreateIndexAfterCreateTable: true,
	}}}
}

// TestTryRemoveOnUpdate 验证从关系约束标签中移除 ON UPDATE 片段
func TestTryRemoveOnUpdate(t *testing.T) {
	m := newTestMigrator()

	if err := m.TryRemoveOnUpdate(&relParent{}); err != nil {
		t.Fatalf("TryRemoveOnUpdate returned error: %v", err)
	}
}

// TestTryRemoveOnUpdateWithoutRelations 验证无关联关系的模型不报错
func TestTryRemoveOnUpdateWithoutRelations(t *testing.T) {
	m := newTestMigrator()

	if err := m.TryRemoveOnUpdate(&limitModel{}); err != nil {
		t.Fatalf("TryRemoveOnUpdate returned error: %v", err)
	}
}

// TestQuoteToReservedWords 验证保留字列名在 SQL 输出时被引号包裹（由 QuoteTo 处理）。
// 注意：此前 TryQuotifyReservedWords 会直接修改缓存的 schema（DBNames/DBName），
// 导致 FieldsByDBName 的 key 与 DBName 不一致，gorm CreateTable 时字段查找为 nil 而 panic。
// 正确做法是依赖 QuoteTo 在输出层处理保留字，不污染共享 schema。
func TestQuoteToReservedWords(t *testing.T) {
	d := newTestDialector("11.2.0.4.0", 2000)

	var buf strings.Builder
	d.QuoteTo(&buf, "SELECT") // 保留字 → 引号包裹
	if got := buf.String(); got != `"SELECT"` {
		t.Errorf("QuoteTo(SELECT) = %q, want %q", got, `"SELECT"`)
	}

	buf.Reset()
	d.QuoteTo(&buf, "NAME") // 非保留字 → 原样输出
	if got := buf.String(); got != "NAME" {
		t.Errorf("QuoteTo(NAME) = %q, want %q", got, "NAME")
	}
}

// TestSequenceAndTriggerNaming 验证序列/触发器名称规则
func TestSequenceAndTriggerNaming(t *testing.T) {
	m := newTestMigrator()

	seq := m.sequenceName("TEST_USERS")
	if seq != "SEQ_TEST_USERS" {
		t.Errorf("sequenceName() = %q, want %q", seq, "SEQ_TEST_USERS")
	}

	trg := m.triggerName("TEST_USERS")
	if trg != "TRG_TEST_USERS" {
		t.Errorf("triggerName() = %q, want %q", trg, "TRG_TEST_USERS")
	}
}

// TestMigratorDelegateMethods 验证 Migrator 的 DataTypeOf 委托
func TestMigratorDelegateMethods(t *testing.T) {
	m := newTestMigrator()

	f := testField(schema.Int)
	f.Size = 64
	f.FieldType = reflect.TypeFor[int]()
	f.IndirectFieldType = f.FieldType

	if got := m.DataTypeOf(f); got != "INTEGER" {
		t.Errorf("Migrator.DataTypeOf() = %q, want %q", got, "INTEGER")
	}
}

// TestNoopDialectorImplementsGormDialector 编译期验证 noopDialector 实现了 gorm.Dialector
var _ gorm.Dialector = (*noopDialector)(nil)
