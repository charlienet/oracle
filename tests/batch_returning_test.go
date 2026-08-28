package tests

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

// SeqBatchModel 非显式 autoIncrement 主键模型。
// GORM 会把这类 int/uint 主键自动视为自增（schema.go:337-348：设 AutoIncrement=true、
// HasDefaultValue=true 并加入 FieldsWithDefaultDBValue），因此批量 Create 时
// create.go 会生成 INSERT ... RETURNING id INTO :out，走 RETURNING INTO 回填路径。
type SeqBatchModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"size:100"`
}

func (SeqBatchModel) TableName() string {
	return "TEST_SEQ_BATCH"
}

// SeqMultiDefaultModel 含两个默认值字段的模型：非显式 autoIncrement 主键（ID）+ 括号序列
// 默认值字段（Code）。两者都进入 FieldsWithDefaultDBValue，批量 Create 时
// create.go 生成 RETURNING id, code INTO :1, :2 —— 两个 sql.Out 输出参数。
type SeqMultiDefaultModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Code int    `gorm:"column:code;default:(SEQ_TEST_SEQ_MULTI_CODE.NEXTVAL)"`
	Name string `gorm:"size:100"`
}

func (SeqMultiDefaultModel) TableName() string {
	return "TEST_SEQ_MULTI"
}

// setupSeqMultiTable 建立两个序列默认值字段的表结构（ID 与 Code 各配一个 BEFORE INSERT 触发器）。
func setupSeqMultiTable(t *testing.T) {
	t.Helper()
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_MULTI_CODE")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_MULTI")
	_ = DB.Migrator().DropTable(&SeqMultiDefaultModel{})

	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_SEQ_MULTI START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_SEQ_MULTI_CODE START WITH 500 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_SEQ_MULTI (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		code NUMBER(19),
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_TEST_SEQ_MULTI
BEFORE INSERT ON TEST_SEQ_MULTI
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_SEQ_MULTI.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`).Error; err != nil {
		t.Fatalf("failed to create id trigger: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_TEST_SEQ_MULTI_CODE
BEFORE INSERT ON TEST_SEQ_MULTI
FOR EACH ROW
BEGIN
	IF :NEW.code IS NULL THEN
		SELECT SEQ_TEST_SEQ_MULTI_CODE.NEXTVAL INTO :NEW.code FROM DUAL;
	END IF;
END;`).Error; err != nil {
		t.Fatalf("failed to create code trigger: %v", err)
	}
}

func teardownSeqMultiTable() {
	DB.Exec("DROP TABLE TEST_SEQ_MULTI")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_MULTI_CODE")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_MULTI")
}

// BlackListModel 模拟应用场景模型：ID 为主键（非显式 autoIncrement，自动进入
// FieldsWithDefaultDBValue），BlackID 为被 Omit 的业务字段。
type BlackListModel struct {
	ID      uint   `gorm:"column:id;primaryKey"`
	BlackID uint   `gorm:"column:black_id"`
	Name    string `gorm:"size:100"`
}

func (BlackListModel) TableName() string {
	return "TEST_BLACK_LIST"
}

// TestCreateInBatchesWithOmit 模拟应用真实调用：
// tx.Omit("BlackID").CreateInBatches(records, batchSize)
func TestCreateInBatchesWithOmit(t *testing.T) {
	_ = DB.Migrator().DropTable(&BlackListModel{})
	if err := DB.AutoMigrate(&BlackListModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&BlackListModel{}) }()
	clearTable(t, "TEST_BLACK_LIST")

	// 11 条记录，batchSize=3，跨 4 批；BlackID 赋非零值但被 Omit
	records := make([]BlackListModel, 0, 11)
	for i := range 11 {
		records = append(records, BlackListModel{BlackID: uint(i + 1), Name: "Omit-" + string(rune('A'+i))})
	}

	result := DB.Omit("BlackID").CreateInBatches(records, 3)
	if result.Error != nil {
		t.Fatalf("CreateInBatches with Omit failed: %v", result.Error)
	}
	if result.RowsAffected != 11 {
		t.Errorf("expected 11 rows affected, got %d", result.RowsAffected)
	}
	for i, r := range records {
		if r.ID == 0 {
			t.Errorf("record %d: expected ID to be set via RETURNING", i)
		}
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_BLACK_LIST").Scan(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 11 {
		t.Errorf("expected 11 rows in db, got %d", count)
	}
	// BlackID 被 Omit，DB 端应为 NULL
	var blackIDs []int64
	if err := DB.Raw("SELECT black_id FROM TEST_BLACK_LIST WHERE black_id IS NOT NULL").Scan(&blackIDs).Error; err != nil {
		t.Fatalf("failed to query black_id: %v", err)
	}
	if len(blackIDs) != 0 {
		t.Errorf("expected black_id to be NULL after Omit, got %v", blackIDs)
	}
	t.Logf("create %d rows OK, IDs %d..%d", len(records), records[0].ID, records[len(records)-1].ID)
}

// StrDefaultModel 含字符串 DB 默认值字段：default:('INIT') 含括号 → DefaultValueInterface=nil
// → 进入 FieldsWithDefaultDBValue。RETURNING 输出列含 VARCHAR2 类型，go-ora 对
// 字符串 Out 参数需要 Size（adapter.NeedsSizeForOut()==true），而 create.go 的
// sql.Out 无法携带 Size（database/sql.Out 无 Size 字段）。
type StrDefaultModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Code string `gorm:"column:code;size:100;default:('INIT')"`
	Name string `gorm:"size:100"`
}

func (StrDefaultModel) TableName() string {
	return "TEST_STR_DEF"
}

// TestCreateBatchStrDefault 字符串默认值字段批量插入（RETURNING 输出 VARCHAR2）。
// 建表用原生 SQL（绕过 migrator 对括号默认值的引号处理缺陷），
// ID 用序列+触发器回填，code 列用 DEFAULT 'INIT'。
func TestCreateBatchStrDefault(t *testing.T) {
	// 原生 SQL 清理（带重试，避免偶发的 DDL 锁）
	for range 5 {
		if err := DB.Exec("DROP TABLE TEST_STR_DEF PURGE").Error; err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	DB.Exec("DROP SEQUENCE SEQ_TEST_STR_DEF")
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_STR_DEF START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_STR_DEF (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		code VARCHAR2(100) DEFAULT 'INIT',
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_TEST_STR_DEF
BEFORE INSERT ON TEST_STR_DEF
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_STR_DEF.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}
	defer func() {
		DB.Exec("DROP TABLE TEST_STR_DEF PURGE")
		DB.Exec("DROP SEQUENCE SEQ_TEST_STR_DEF")
	}()

	items := []StrDefaultModel{
		{Name: "Str 1"},
		{Name: "Str 2"},
		{Name: "Str 3"},
	}
	result := DB.Create(&items)
	if result.Error != nil {
		t.Fatalf("batch create with VARCHAR2 RETURNING failed: %v", result.Error)
	}
	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}
	for i, it := range items {
		if it.ID == 0 {
			t.Errorf("item %d: expected ID set via RETURNING", i)
		}
		t.Logf("item %d: ID=%d Code=%q", i, it.ID, it.Code)
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_STR_DEF").Scan(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows in db, got %d", count)
	}
}

// BlackDefaultModel 模拟 BlackID 在 FieldsWithDefaultDBValue 中（带括号 DB 默认值）但被 Omit 的场景：
// gorm 的 columns 不含 black_id，但 create.go 的 RETURNING 仍基于完整
// FieldsWithDefaultDBValue 生成 RETURNING black_id, id INTO —— 输出参数与实际 INSERT 列不匹配。
type BlackDefaultModel struct {
	ID      uint   `gorm:"column:id;primaryKey"`
	BlackID uint   `gorm:"column:black_id;default:(0)"`
	Name    string `gorm:"size:100"`
}

func (BlackDefaultModel) TableName() string {
	return "TEST_BLACK_DEF"
}

// TestCreateInBatchesOmitDefaultField Omit 一个带 DB 默认值的字段（在 FieldsWithDefaultDBValue 中）
// 后 CreateInBatches 批量插入。
func TestCreateInBatchesOmitDefaultField(t *testing.T) {
	for range 5 {
		if err := DB.Exec("DROP TABLE TEST_BLACK_DEF PURGE").Error; err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	DB.Exec("DROP SEQUENCE SEQ_TEST_BLACK_DEF")
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_BLACK_DEF START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_BLACK_DEF (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		black_id NUMBER(19) DEFAULT 0,
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_TEST_BLACK_DEF
BEFORE INSERT ON TEST_BLACK_DEF
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_BLACK_DEF.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}
	defer func() {
		DB.Exec("DROP TABLE TEST_BLACK_DEF PURGE")
		DB.Exec("DROP SEQUENCE SEQ_TEST_BLACK_DEF")
	}()

	records := make([]BlackDefaultModel, 0, 6)
	for i := range 6 {
		records = append(records, BlackDefaultModel{BlackID: uint(i + 1), Name: "BD-" + string(rune('A'+i))})
	}
	result := DB.Omit("BlackID").CreateInBatches(records, 3)
	if result.Error != nil {
		t.Fatalf("CreateInBatches with Omit(default field) failed: %v", result.Error)
	}
	if result.RowsAffected != 6 {
		t.Errorf("expected 6 rows affected, got %d", result.RowsAffected)
	}
	for i, r := range records {
		if r.ID == 0 {
			t.Errorf("record %d: expected ID set via RETURNING", i)
		}
	}
	t.Logf("omit-default batch OK, IDs %d..%d", records[0].ID, records[len(records)-1].ID)
}

// TimeDefaultModel 含时间戳 DB 默认值字段：default:(SYSDATE) 含括号 → DefaultValueInterface=nil
// → 进入 FieldsWithDefaultDBValue。RETURNING 输出列含 DATE 类型。
type TimeDefaultModel struct {
	ID        uint      `gorm:"column:id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at;default:(SYSDATE)"`
	Name      string    `gorm:"size:100"`
}

func (TimeDefaultModel) TableName() string {
	return "TEST_TIME_DEF"
}

// TestCreateBatchTimeDefault 时间戳默认值字段批量插入（RETURNING 输出 DATE）。
func TestCreateBatchTimeDefault(t *testing.T) {
	for range 5 {
		if err := DB.Exec("DROP TABLE TEST_TIME_DEF PURGE").Error; err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	DB.Exec("DROP SEQUENCE SEQ_TEST_TIME_DEF")
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_TIME_DEF START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_TIME_DEF (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		created_at DATE DEFAULT SYSDATE,
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_TEST_TIME_DEF
BEFORE INSERT ON TEST_TIME_DEF
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_TIME_DEF.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}
	defer func() {
		DB.Exec("DROP TABLE TEST_TIME_DEF PURGE")
		DB.Exec("DROP SEQUENCE SEQ_TEST_TIME_DEF")
	}()

	items := []TimeDefaultModel{
		{Name: "T1"}, {Name: "T2"}, {Name: "T3"},
	}
	result := DB.Create(&items)
	if result.Error != nil {
		t.Fatalf("batch create with DATE RETURNING failed: %v", result.Error)
	}
	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}
	for i, it := range items {
		if it.ID == 0 {
			t.Errorf("item %d: expected ID set via RETURNING", i)
		}
	}
	t.Logf("time-default batch OK, IDs %d..%d", items[0].ID, items[2].ID)
}

// MerchantBlackMock 模拟用户应用模型：非显式 autoIncrement 主键（ID 进入
// FieldsWithDefaultDBValue，任何删除路径都会生成 RETURNING）+ 软删除字段。
// 与用户场景一致：事务内先按非主键条件 Delete（可能多行），再 CreateInBatches。
type MerchantBlackMock struct {
	ID        uint           `gorm:"column:id;primaryKey"`
	BlackID   uint           `gorm:"column:black_id"`
	BlackType string         `gorm:"column:black_type;size:20"`
	Reference string         `gorm:"column:reference;size:100"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (MerchantBlackMock) TableName() string {
	return "TEST_MERCHANT_BLACK"
}

// TestSoftDeleteMultiRowReturning 重现：事务内先多行软删除（WHERE 匹配多行），
// 软删除 UPDATE ... RETURNING id INTO :out 因多行受影响触发
// "more than one row affected with return clause"。
func TestSoftDeleteMultiRowReturning(t *testing.T) {
	_ = DB.Migrator().DropTable(&MerchantBlackMock{})
	if err := DB.AutoMigrate(&MerchantBlackMock{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&MerchantBlackMock{}) }()

	// 插入 3 行相同 BLACK_TYPE
	items := []MerchantBlackMock{
		{BlackID: 1, BlackType: "M", Reference: "R1"},
		{BlackID: 2, BlackType: "M", Reference: "R2"},
		{BlackID: 3, BlackType: "M", Reference: "R3"},
	}
	if err := DB.Create(&items).Error; err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	// 模拟用户场景：同一事务内先多行 Delete 再 CreateInBatches
	records := []MerchantBlackMock{
		{BlackID: 10, BlackType: "M", Reference: "N1"},
		{BlackID: 11, BlackType: "M", Reference: "N2"},
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("BLACK_TYPE = ?", "M").Delete(&MerchantBlackMock{}).Error; err != nil {
			return err
		}
		return tx.Omit("BlackID").CreateInBatches(records, 2).Error
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// 旧 3 行应已软删除（deleted_at 非空）
	var deleted int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_MERCHANT_BLACK WHERE BLACK_TYPE = 'M' AND DELETED_AT IS NOT NULL").Scan(&deleted).Error; err != nil {
		t.Fatalf("failed to count deleted: %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 soft-deleted rows, got %d", deleted)
	}
	// 新 2 行应已插入
	var alive int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_MERCHANT_BLACK WHERE BLACK_TYPE = 'M' AND DELETED_AT IS NULL").Scan(&alive).Error; err != nil {
		t.Fatalf("failed to count alive: %v", err)
	}
	if alive != 2 {
		t.Errorf("expected 2 inserted rows, got %d", alive)
	}
	for i, r := range records {
		if r.ID == 0 {
			t.Errorf("record %d: expected ID set via RETURNING", i)
		}
	}
	t.Logf("transaction OK: soft-deleted %d, inserted IDs %d..%d", deleted, records[0].ID, records[1].ID)
}

// MerchantBlackHardMock 无软删除字段 → Delete 走硬删除路径（DELETE...RETURNING）。
type MerchantBlackHardMock struct {
	ID        uint   `gorm:"column:id;primaryKey"`
	BlackID   uint   `gorm:"column:black_id"`
	BlackType string `gorm:"column:black_type;size:20"`
	Reference string `gorm:"column:reference;size:100"`
}

func (MerchantBlackHardMock) TableName() string {
	return "TEST_MERCHANT_BLACK_HARD"
}

// TestHardDeleteMultiRowReturning 重现：硬删除按非主键条件匹配多行，
// DELETE ... RETURNING id INTO :out 因多行受影响触发报错。
func TestHardDeleteMultiRowReturning(t *testing.T) {
	_ = DB.Migrator().DropTable(&MerchantBlackHardMock{})
	if err := DB.AutoMigrate(&MerchantBlackHardMock{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&MerchantBlackHardMock{}) }()

	items := []MerchantBlackHardMock{
		{BlackID: 1, BlackType: "M", Reference: "R1"},
		{BlackID: 2, BlackType: "M", Reference: "R2"},
		{BlackID: 3, BlackType: "M", Reference: "R3"},
	}
	if err := DB.Create(&items).Error; err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		return tx.Where("BLACK_TYPE = ?", "M").Delete(&MerchantBlackHardMock{}).Error
	})
	if err != nil {
		t.Fatalf("multi-row hard delete failed: %v", err)
	}
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_MERCHANT_BLACK_HARD").Scan(&count).Error; err != nil {
		t.Fatalf("failed to count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows after hard delete, got %d", count)
	}
	t.Logf("multi-row hard delete OK")
}

// TestCreateBatchMultiReturning 重现：多个默认值字段（RETURNING 多输出参数）批量插入。
func TestCreateBatchMultiReturning(t *testing.T) {
	setupSeqMultiTable(t)
	defer teardownSeqMultiTable()

	items := []SeqMultiDefaultModel{
		{Name: "Multi 1"},
		{Name: "Multi 2"},
		{Name: "Multi 3"},
	}

	result := DB.Create(&items)
	if result.Error != nil {
		t.Fatalf("batch create with multi RETURNING INTO failed: %v", result.Error)
	}
	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}
	for i, it := range items {
		if it.ID == 0 {
			t.Errorf("item %d: expected ID to be set via RETURNING", i)
		}
		if it.Code == 0 {
			t.Errorf("item %d: expected Code to be set via RETURNING", i)
		}
	}
	t.Logf("batch IDs: %d,%d,%d Codes: %d,%d,%d",
		items[0].ID, items[1].ID, items[2].ID,
		items[0].Code, items[1].Code, items[2].Code)
}

// setupSeqBatchTable 建立批量 RETURNING INTO 测试所需的表结构：
// 11g 下使用 BEFORE INSERT 触发器从序列回填主键（与 TestExplicitSequenceDefaultValue 同方案）。
func setupSeqBatchTable(t *testing.T) {
	t.Helper()
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_BATCH")
	_ = DB.Migrator().DropTable(&SeqBatchModel{})

	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_SEQ_BATCH START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_SEQ_BATCH (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	triggerSQL := `CREATE OR REPLACE TRIGGER TRG_TEST_SEQ_BATCH
BEFORE INSERT ON TEST_SEQ_BATCH
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_SEQ_BATCH.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`
	if err := DB.Exec(triggerSQL).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}
}

func teardownSeqBatchTable() {
	DB.Exec("DROP TABLE TEST_SEQ_BATCH")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_BATCH")
}

// TestCreateBatchReturningInto 重现：批量插入非显式 autoIncrement 主键模型时，
// create.go 逐行执行 INSERT ... RETURNING id INTO :out，sql.Out 参数在循环中
// 未重置，可能导致 go-ora 报 "more than one row affected with return clause"。
func TestCreateBatchReturningInto(t *testing.T) {
	setupSeqBatchTable(t)
	defer teardownSeqBatchTable()

	items := []SeqBatchModel{
		{Name: "Batch 1"},
		{Name: "Batch 2"},
		{Name: "Batch 3"},
	}

	result := DB.Create(&items)
	if result.Error != nil {
		t.Fatalf("batch create with RETURNING INTO failed: %v", result.Error)
	}

	if result.RowsAffected != 3 {
		t.Errorf("expected 3 rows affected, got %d", result.RowsAffected)
	}

	// 每行的 ID 都应被 RETURNING 回填，且严格递增（由序列保证）
	for i, it := range items {
		if it.ID == 0 {
			t.Errorf("item %d: expected ID to be set via RETURNING", i)
		}
		if i > 0 && it.ID != items[i-1].ID+1 {
			t.Errorf("expected IDs to be sequential, got items[%d].ID=%d, items[%d].ID=%d",
				i-1, items[i-1].ID, i, it.ID)
		}
	}
	t.Logf("batch IDs: %d, %d, %d", items[0].ID, items[1].ID, items[2].ID)

	// 数据库端交叉验证：三行都已落库，ID 与内存回填一致
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM TEST_SEQ_BATCH").Scan(&count).Error; err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows in database, got %d", count)
	}
	for i, it := range items {
		var dbID uint
		if err := DB.Raw("SELECT id FROM TEST_SEQ_BATCH WHERE name = ?", it.Name).Scan(&dbID).Error; err != nil {
			t.Fatalf("failed to query id for %s: %v", it.Name, err)
		}
		if dbID != it.ID {
			t.Errorf("item %d: database id=%d != in-memory id=%d", i, dbID, it.ID)
		}
	}
}

// TestCreateBatchReturningIntoLarge 放大批量行数，提高对偶发问题的检出率。
func TestCreateBatchReturningIntoLarge(t *testing.T) {
	setupSeqBatchTable(t)
	defer teardownSeqBatchTable()

	items := make([]SeqBatchModel, 0, 10)
	for range 10 {
		items = append(items, SeqBatchModel{Name: "Batch L"})
	}

	result := DB.Create(&items)
	if result.Error != nil {
		t.Fatalf("batch create with RETURNING INTO failed: %v", result.Error)
	}
	for i, it := range items {
		if it.ID == 0 {
			t.Errorf("item %d: expected ID to be set via RETURNING", i)
		}
	}
	t.Logf("large batch IDs: %d ... %d", items[0].ID, items[len(items)-1].ID)
}
