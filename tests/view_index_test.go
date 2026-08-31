package tests

// 本文件固定 Migrator().CreateView / CreateIndex 在 Oracle 上的行为：
// 本库未覆写这两个方法（依赖 gorm 默认实现），而 gorm 默认 SQL（CREATE VIEW /
// CREATE INDEX，无 IF NOT EXISTS 子句）恰与 Oracle 兼容。
// 此处用集成测试锁定该事实，防止未来 gorm 升级或本库误覆写引入语法回归
//（如生成 IF NOT EXISTS、错误的引号风格等）。
// 相关已覆写方法：DropView（PL/SQL 幂等，migrator.go）、DropIndex/HasIndex/GetIndexes
//（migrator.go）见各文件注释。

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestCreateIndexTable / TestCreateIndexWithIdx 共享表 TEST_CREATE_INDEX：
// 前者用于 AutoMigrate 建表（不含目标索引，避免 AutoMigrate 自动建索引）；
// 后者带索引定义，供 Migrator().CreateIndex 的 schema.LookIndex 命中（gorm 默认
// 实现要求索引名在模型 schema 中有定义，否则返回 "failed to create index"）。
type TestCreateIndexTable struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name;size:50"`
}

func (TestCreateIndexTable) TableName() string { return "TEST_CREATE_INDEX" }

type TestCreateIndexWithIdx struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name;size:50;index:idx_test_create_index_name"`
}

func (TestCreateIndexWithIdx) TableName() string { return "TEST_CREATE_INDEX" }

// TestCreateViewBase 视图的基表（视图查询的数据来源）
type TestCreateViewBase struct {
	ID   uint   `gorm:"column:id;primaryKey;autoIncrement"`
	Name string `gorm:"column:name;size:50"`
}

// viewTypeModel 供 TableType 查询视图类型（TableName 指向测试视图名 TEST_CREATE_VIEW）
type viewTypeModel struct{}

func (viewTypeModel) TableName() string { return "TEST_CREATE_VIEW" }

// TestCreateView 验证 Migrator().CreateView（gorm 默认实现）在 Oracle 上可用：
// 生成的 SQL 为 CREATE VIEW <name> AS <子查询>（无 IF NOT EXISTS）；
// 创建后可被 TableType 识别为 "VIEW"、可查询；DropView 后视图消失。
func TestCreateView(t *testing.T) {
	// 1. 准备基表
	if err := DB.AutoMigrate(&TestCreateViewBase{}); err != nil {
		t.Fatalf("failed to create base table: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&TestCreateViewBase{}) }()

	const viewName = "TEST_CREATE_VIEW"

	// 2. 调用 Migrator().CreateView（Query 为子查询）
	subQuery := DB.Model(&TestCreateViewBase{}).Select("id", "name")
	if err := DB.Migrator().CreateView(viewName, gorm.ViewOption{Query: subQuery}); err != nil {
		t.Fatalf("CreateView returned error: %v", err)
	}
	defer func() { _ = DB.Migrator().DropView(viewName) }()

	// 3. 断言 TableType 识别为 VIEW
	tt, err := DB.Migrator().TableType(&viewTypeModel{})
	if err != nil {
		t.Fatalf("TableType returned error: %v", err)
	}
	if !strings.EqualFold(tt.Type(), "VIEW") {
		t.Errorf("expected table type VIEW, got %q", tt.Type())
	}

	// 4. 插入基础数据，断言视图可查询且数据可达
	if err := DB.Create(&TestCreateViewBase{Name: "view-row"}).Error; err != nil {
		t.Fatalf("failed to insert base row: %v", err)
	}
	var viewCount int64
	if err := DB.Raw("SELECT COUNT(*) FROM " + viewName).Scan(&viewCount).Error; err != nil {
		t.Fatalf("query view failed: %v", err)
	}
	if viewCount != 1 {
		t.Errorf("expected 1 row visible through view, got %d", viewCount)
	}

	// 5. DropView 后视图不存在（TableType 应报错）
	if err := DB.Migrator().DropView(viewName); err != nil {
		t.Fatalf("DropView returned error: %v", err)
	}
	if tt, err := DB.Migrator().TableType(&viewTypeModel{}); err == nil {
		t.Errorf("expected TableType error after DropView, got type %q", tt.Type())
	}
}

// TestCreateIndex 验证 Migrator().CreateIndex（gorm 默认实现）在 Oracle 上可用：
// 生成的 SQL 为 CREATE INDEX <name> ON <table>(<cols>)（无 IF NOT EXISTS）；
// 创建后 HasIndex=true、GetIndexes 含该索引；DropIndex 后 HasIndex=false。
func TestCreateIndex(t *testing.T) {
	// 1. 建表（无目标索引）
	if err := DB.AutoMigrate(&TestCreateIndexTable{}); err != nil {
		t.Fatalf("failed to create test table: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&TestCreateIndexTable{}) }()

	const idxName = "idx_test_create_index_name"

	// 2. 调用 Migrator().CreateIndex（gorm 默认实现，需 schema 含该索引定义）
	if err := DB.Migrator().CreateIndex(&TestCreateIndexWithIdx{}, idxName); err != nil {
		t.Fatalf("CreateIndex returned error: %v", err)
	}
	defer func() { _ = DB.Migrator().DropIndex(&TestCreateIndexWithIdx{}, idxName) }()

	// 3. 断言 HasIndex 为 true
	if !DB.Migrator().HasIndex(&TestCreateIndexWithIdx{}, idxName) {
		t.Error("expected index idx_test_create_index_name to exist after CreateIndex")
	}

	// 4. 断言 GetIndexes 列表包含该索引
	indexes, err := DB.Migrator().GetIndexes(&TestCreateIndexWithIdx{})
	if err != nil {
		t.Fatalf("GetIndexes returned error: %v", err)
	}
	found := false
	for _, idx := range indexes {
		if strings.EqualFold(idx.Name(), "IDX_TEST_CREATE_INDEX_NAME") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected index IDX_TEST_CREATE_INDEX_NAME in GetIndexes, got %v", indexes)
	}

	// 5. DropIndex 后 HasIndex 为 false
	if err := DB.Migrator().DropIndex(&TestCreateIndexWithIdx{}, idxName); err != nil {
		t.Fatalf("DropIndex returned error: %v", err)
	}
	if DB.Migrator().HasIndex(&TestCreateIndexWithIdx{}, idxName) {
		t.Error("expected index to be gone after DropIndex")
	}
}
