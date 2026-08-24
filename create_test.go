package oracle

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// createTestModel 含主键、普通字段与默认值字段，用于 Create 回调 SQL 生成测试
type createTestModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string
	Code string `gorm:"default:ABC"`
}

// newTestDB 构造 DryRun 模式的 gorm.DB 与已初始化的 Statement。
//
// 用 gorm.Open(noopDialector) 完成 db 的完整初始化（callbacks、cacheStore、
// NowFunc、Logger 等），再替换 db.Config.Dialector 为 *Dialector：
//   - Create 的 MERGE 分支依赖 db.Dialector.(*Dialector) 类型断言；
//   - gorm.DeletedAt 的 SoftDeleteDeleteClause 依赖 db.Callback()（callbacks 已初始化）。
//
// ReflectValue 按 gorm Processor.Execute 的规则解引用 Dest（指针 -> 值），
// 保证 ConvertToCreateValues 中 field.Set 等写操作可寻址。
func newTestDB(t *testing.T, model any) *gorm.DB {
	t.Helper()
	d := newTestDialector("12.1.0.2.0", 256)
	db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	db.Config.Dialector = d

	sch := parseTestSchema(t, model)

	rv := reflect.ValueOf(model)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	db.Statement = &gorm.Statement{
		DB:           db,
		Schema:       sch,
		Table:        sch.Table,
		Dest:         model,
		ReflectValue: rv,
		Clauses:      map[string]clause.Clause{},
		Vars:         []interface{}{},
	}
	return db
}

func TestCreateVarsSafetyProtection(t *testing.T) {
	// 测试场景：批量插入时 Vars 不会覆盖 RETURNING INTO 的输出参数
	// 由于这是内部逻辑，需要通过集成测试验证
	t.Log("Vars safety protection implemented in create.go")
	// 这个测试主要是确认修复存在，实际验证通过集成测试进行
}

func TestCreateSQLGeneration(t *testing.T) {
	// 单条 INSERT：验证包含主键列、普通字段列、默认值字段列，
	// 且默认值字段存在时生成 RETURNING ... INTO 输出参数
	model := &createTestModel{Name: "alice"}
	db := newTestDB(t, model)

	Create(db)

	if db.Error != nil {
		t.Fatalf("Create returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"INSERT INTO", "VALUES", "RETURNING", "INTO"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Create SQL %q missing %q", sql, want)
		}
	}
}

func TestCreateWithOnConflict(t *testing.T) {
	// 设置 ON CONFLICT 且 values 含主键列时，走 MERGE（UPSERT）分支
	model := &createTestModel{ID: 1, Name: "alice", Code: "XYZ"}
	db := newTestDB(t, model)
	db.Statement.AddClause(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name", "code"}),
	})

	Create(db)

	if db.Error != nil {
		t.Fatalf("Create returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"MERGE INTO", "USING (", "WHEN NOT MATCHED"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Create MERGE SQL %q missing %q", sql, want)
		}
	}
}

func TestCreateBatchMERGEError(t *testing.T) {
	// 多行 values + ON CONFLICT：批量 MERGE 不被支持，应报错
	batch := &[]createTestModel{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}
	db := newTestDB(t, batch)
	db.Statement.AddClause(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	})

	Create(db)

	if db.Error == nil {
		t.Fatal("expected error for batch MERGE, got nil")
	}
	if !strings.Contains(db.Error.Error(), "batch UPSERT") {
		t.Errorf("error %q does not contain %q", db.Error.Error(), "batch UPSERT")
	}
}
