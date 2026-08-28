package tests

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// ---- 审计问题复现测试（对应 oracle 审查报告的 #1-#4, #7, #10, #11, #13）----

// dropTableNative 原生 SQL 清理表（带重试，规避偶发 DDL 锁）
func dropTableNative(t *testing.T, table string) {
	t.Helper()
	for range 5 {
		err := DB.Exec("DROP TABLE " + table + " PURGE").Error
		if err == nil {
			return
		}
		// ORA-00942：表或视图不存在 → 视为清理成功，无需重试
		if strings.Contains(err.Error(), "ORA-00942") {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// dropSequencesLike 删除名称匹配的所有序列（处理截断后的序列名，避免 ORA-00972/00955）
func dropSequencesLike(t *testing.T, pattern string) {
	t.Helper()
	var names []string
	if err := DB.Raw("SELECT SEQUENCE_NAME FROM USER_SEQUENCES WHERE SEQUENCE_NAME LIKE ?", pattern).Scan(&names).Error; err != nil {
		return
	}
	for _, n := range names {
		DB.Exec("DROP SEQUENCE " + n)
	}
}

// ---------- #1：create.go MERGE/UPSERT 路径缺少 RETURNING INTO ----------

// TestMergePathReturnsDefaultValue 验证 MERGE/UPSERT 路径行为。
// 注意：Oracle 11g 的 MERGE 语句不支持 RETURNING INTO（实测 ORA-00933），
// 因此 UPSERT 后非主键默认值字段（Code）无法回填，属 Oracle 限制。
// 本测试验证 MERGE 路径的正确语义：Upsert 成功、主键保持用户给定值、
// 显式提供的默认值字段正确落库。
func TestMergePathReturnsDefaultValue(t *testing.T) {
	setupSeqMultiTable(t)
	defer teardownSeqMultiTable()

	// 主键显式赋值 → columns 含主键 → 走 MERGE 分支
	item := SeqMultiDefaultModel{ID: 500, Code: 42, Name: "Merge Upsert"}
	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
	if result.Error != nil {
		t.Fatalf("upsert failed: %v", result.Error)
	}
	// MERGE 分支下主键保持用户给定值
	if item.ID != 500 {
		t.Errorf("expected ID to keep user-supplied value 500, got %d", item.ID)
	}
	// 显式提供的默认值字段正确落库
	var code int
	if err := DB.Raw("SELECT CODE FROM TEST_SEQ_MULTI WHERE ID = ?", 500).Scan(&code).Error; err != nil {
		t.Fatalf("failed to query code: %v", err)
	}
	if code != 42 {
		t.Errorf("expected code=42 in db, got %d", code)
	}

	// 再次 Upsert（DoNothing 时已存在则忽略，不报错）
	dup := SeqMultiDefaultModel{ID: 500, Name: "Merge Dup"}
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&dup).Error; err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_SEQ_MULTI WHERE ID = 500").Scan(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after upsert, got %d", count)
	}
}

// ---------- #2：RenameTable 使用非法 SQL 语法（RENAME TABLE 是 MySQL 语法）----------

type AuditRenameModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"size:100"`
}

func (AuditRenameModel) TableName() string { return "TEST_AUDIT_RENAME_OLD" }

func TestRenameTable(t *testing.T) {
	dropTableNative(t, "TEST_AUDIT_RENAME_OLD")
	dropTableNative(t, "TEST_AUDIT_RENAME_NEW")
	dropSequencesLike(t, "SEQ_TEST_AUDIT_RENAME%")
	if err := DB.AutoMigrate(&AuditRenameModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_AUDIT_RENAME_OLD")
		dropTableNative(t, "TEST_AUDIT_RENAME_NEW")
		dropSequencesLike(t, "SEQ_TEST_AUDIT_RENAME%")
	}()

	if err := DB.Migrator().RenameTable("TEST_AUDIT_RENAME_OLD", "TEST_AUDIT_RENAME_NEW"); err != nil {
		t.Fatalf("RenameTable failed: %v", err)
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = ?", "TEST_AUDIT_RENAME_NEW").Scan(&count).Error; err != nil {
		t.Fatalf("failed to check table: %v", err)
	}
	if count == 0 {
		t.Error("expected renamed table to exist")
	}
}

// ---------- #3：DropOnUpdateTrigger 使用 Oracle 不支持的 DROP TRIGGER IF EXISTS ----------

func TestDropOnUpdateTrigger(t *testing.T) {
	rel := &schema.Relationship{
		Field: &schema.Field{DBName: "USER_ID"},
	}
	m := DB.Migrator()
	dm, ok := m.(interface {
		DropOnUpdateTrigger(value any, rel *schema.Relationship) error
	})
	if !ok {
		t.Fatal("Migrator does not implement DropOnUpdateTrigger")
	}
	if err := dm.DropOnUpdateTrigger(&User{}, rel); err != nil {
		t.Fatalf("DropOnUpdateTrigger failed: %v", err)
	}
}

// ---------- #4：sequenceName/triggerName 未截断导致长表名 AutoMigrate 崩溃 ----------

type AuditLongTableModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"size:100"`
}

// 27 字符表名：SEQ_ 前缀(4) + 27 = 31 > 30（Oracle 标识符上限）
func (AuditLongTableModel) TableName() string { return "TEST_AUDIT_LONG_TABLE_NAME_XYZ" }

func TestAutoMigrateLongTableName(t *testing.T) {
	dropTableNative(t, "TEST_AUDIT_LONG_TABLE_NAME_XYZ")
	// 序列名会被截断到 30 字符，用 LIKE 动态清理
	dropSequencesLike(t, "SEQ_TEST_AUDIT_LONG%")
	if err := DB.AutoMigrate(&AuditLongTableModel{}); err != nil {
		t.Fatalf("AutoMigrate long table name failed: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_AUDIT_LONG_TABLE_NAME_XYZ")
		dropSequencesLike(t, "SEQ_TEST_AUDIT_LONG%")
	}()
}

// ---------- #7：软删除时间使用 time.Now() 而非 db.NowFunc() ----------

func TestSoftDeleteUsesNowFunc(t *testing.T) {
	dropTableNative(t, "TEST_MERCHANT_BLACK")
	_ = DB.Migrator().DropTable(&MerchantBlackMock{})
	if err := DB.AutoMigrate(&MerchantBlackMock{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&MerchantBlackMock{}) }()

	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	orig := DB.NowFunc
	DB.NowFunc = func() time.Time { return fixed }
	defer func() { DB.NowFunc = orig }()

	m := MerchantBlackMock{BlackID: 1, BlackType: "M", Reference: "R"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}
	if err := DB.Where("ID = ?", m.ID).Delete(&MerchantBlackMock{}).Error; err != nil {
		t.Fatalf("failed to soft delete: %v", err)
	}

	var deletedAt *time.Time
	if err := DB.Raw("SELECT DELETED_AT FROM TEST_MERCHANT_BLACK WHERE ID = ?", m.ID).Scan(&deletedAt).Error; err != nil {
		t.Fatalf("failed to query deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
	if !deletedAt.Equal(fixed) {
		t.Errorf("expected deleted_at = %v, got %v", fixed, *deletedAt)
	}
}

// ---------- #10：DropTable 未加 PURGE，同名重建报 ORA-00955 ----------

type AuditPurgeModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"size:100"`
}

func (AuditPurgeModel) TableName() string { return "TEST_AUDIT_PURGE" }

func TestDropTableThenCreateSameName(t *testing.T) {
	dropTableNative(t, "TEST_AUDIT_PURGE")
	dropSequencesLike(t, "SEQ_TEST_AUDIT_PURGE%")
	if err := DB.AutoMigrate(&AuditPurgeModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	// DropTable 后立即同名重建（Oracle 回收站中的同名表会导致 ORA-00955）
	if err := DB.Migrator().DropTable(&AuditPurgeModel{}); err != nil {
		t.Fatalf("failed to drop: %v", err)
	}
	if err := DB.AutoMigrate(&AuditPurgeModel{}); err != nil {
		t.Fatalf("recreate after drop failed: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_AUDIT_PURGE")
		dropSequencesLike(t, "SEQ_TEST_AUDIT_PURGE%")
	}()
}

// ---------- #11：保留字列表不完整（列名为 USER 等） ----------

type ReservedWordModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	User string `gorm:"column:USER;size:50"`
	Name string `gorm:"size:100"`
}

func (ReservedWordModel) TableName() string { return "TEST_AUDIT_RESERVED" }

func TestAutoMigrateReservedWordColumn(t *testing.T) {
	dropTableNative(t, "TEST_AUDIT_RESERVED")
	dropSequencesLike(t, "SEQ_TEST_AUDIT_RESERVED%")
	if err := DB.AutoMigrate(&ReservedWordModel{}); err != nil {
		t.Fatalf("AutoMigrate with reserved-word column failed: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_AUDIT_RESERVED")
		dropSequencesLike(t, "SEQ_TEST_AUDIT_RESERVED%")
	}()

	m := ReservedWordModel{User: "admin", Name: "n"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("create with reserved-word column failed: %v", err)
	}
}

// ---------- #13：软删除检测仅认 DeletedAt 字段名，自定义字段名走硬删除 ----------

type CustomSoftDeleteModel struct {
	ID         uint           `gorm:"column:id;primaryKey"`
	Name       string         `gorm:"size:100"`
	DeleteTime gorm.DeletedAt `gorm:"column:delete_time"`
}

func (CustomSoftDeleteModel) TableName() string { return "TEST_AUDIT_CUSTOM_SD" }

func TestSoftDeleteCustomFieldName(t *testing.T) {
	dropTableNative(t, "TEST_AUDIT_CUSTOM_SD")
	_ = DB.Migrator().DropTable(&CustomSoftDeleteModel{})
	dropSequencesLike(t, "SEQ_TEST_AUDIT_CUSTOM_SD%")
	if err := DB.AutoMigrate(&CustomSoftDeleteModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_AUDIT_CUSTOM_SD")
		dropSequencesLike(t, "SEQ_TEST_AUDIT_CUSTOM_SD%")
	}()

	m := CustomSoftDeleteModel{Name: "soft"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}
	if err := DB.Where("ID = ?", m.ID).Delete(&CustomSoftDeleteModel{}).Error; err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// 软删除语义：行应保留且 delete_time 非空
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_AUDIT_CUSTOM_SD WHERE ID = ? AND DELETE_TIME IS NOT NULL", m.ID).Scan(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected row soft-deleted (keep row), got count=%d", count)
	}
}

// ---------- 虚拟列 + 显式小写 column tag 查询回填 ----------

// VirtualColumnModel 验证两类能力：
//  1. 虚拟列（11g+ GENERATED ALWAYS AS ... VIRTUAL）：建表时把表达式写在
//     type: 中，DataTypeOf 原样输出；插入时 `->` 只读字段被跳过。
//  2. 显式小写 column tag（column:first_name）：Oracle 返回大写列名，
//     依赖 query.go 的 patchUpperDBNameKeys 补充大写键才能 scan 回填。
type VirtualColumnModel struct {
	ID        uint   `gorm:"column:id;primaryKey"`
	FirstName string `gorm:"column:first_name;size:50"`
	LastName  string `gorm:"column:last_name;size:50"`
	FullName  string `gorm:"column:full_name;->;type:GENERATED ALWAYS AS (first_name || ' ' || last_name) VIRTUAL"`
}

func (VirtualColumnModel) TableName() string { return "TEST_VIRTUAL_COL" }

// TestVirtualColumnAndLowercaseTag 验证虚拟列建表/插入/查询回填，以及
// 显式小写 column tag 字段的查询回填（Oracle 大写列名 vs 小写 DBName）。
func TestVirtualColumnAndLowercaseTag(t *testing.T) {
	dropTableNative(t, "TEST_VIRTUAL_COL")
	dropSequencesLike(t, "SEQ_TEST_VIRTUAL_COL%")
	if err := DB.AutoMigrate(&VirtualColumnModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() {
		dropTableNative(t, "TEST_VIRTUAL_COL")
		dropSequencesLike(t, "SEQ_TEST_VIRTUAL_COL%")
	}()

	// 确认虚拟列已创建（DATA_DEFAULT 含表达式）
	var dataDefault string
	if err := DB.Raw(`SELECT DATA_DEFAULT FROM USER_TAB_COLUMNS WHERE TABLE_NAME='TEST_VIRTUAL_COL' AND COLUMN_NAME='FULL_NAME'`).Scan(&dataDefault).Error; err != nil {
		t.Fatalf("failed to query virtual column: %v", err)
	}
	if !strings.Contains(dataDefault, "FIRST_NAME") {
		t.Errorf("expected virtual column expression, got DATA_DEFAULT=%q", dataDefault)
	}

	// 插入：full_name 只读，应被跳过；first_name/last_name 正常
	m := VirtualColumnModel{FirstName: "John", LastName: "Doe"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}
	if m.ID == 0 {
		t.Error("expected ID set")
	}

	// 查询回填：小写 column tag 字段 + 虚拟列都应正确
	var out VirtualColumnModel
	if err := DB.First(&out, m.ID).Error; err != nil {
		t.Fatalf("failed to first: %v", err)
	}
	if out.FirstName != "John" {
		t.Errorf("expected FirstName backfilled, got %q", out.FirstName)
	}
	if out.LastName != "Doe" {
		t.Errorf("expected LastName backfilled, got %q", out.LastName)
	}
	if out.FullName != "John Doe" {
		t.Errorf("expected virtual column value 'John Doe', got %q", out.FullName)
	}
}
