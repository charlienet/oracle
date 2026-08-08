package tests

import (
	"testing"
)

// TestSequenceObjectExists 验证 11g 自增依赖的序列对象存在
// （Oracle 对象名默认大写存储，序列命名规则见 migrator.go 的 sequenceName）
func TestSequenceObjectExists(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 验证序列对象存在（11g 自增依赖序列）
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM USER_SEQUENCES WHERE SEQUENCE_NAME = ?", "SEQ_TEST_USERS").Scan(&count).Error; err != nil {
		t.Fatalf("failed to query sequence: %v", err)
	}
	if count == 0 {
		t.Error("expected sequence SEQ_TEST_USERS to exist (auto increment support)")
	}
}

// TestTriggerObjectExists 验证 11g 自增依赖的触发器对象存在
// （触发器命名规则见 migrator.go 的 triggerName）
func TestTriggerObjectExists(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 验证触发器对象存在（11g 自增依赖触发器）
	var count int64
	if err := DB.Raw("SELECT COUNT(*) FROM USER_TRIGGERS WHERE TRIGGER_NAME = ?", "TRG_TEST_USERS").Scan(&count).Error; err != nil {
		t.Fatalf("failed to query trigger: %v", err)
	}
	if count == 0 {
		t.Error("expected trigger TRG_TEST_USERS to exist (auto increment support)")
	}
}

// TestAutoIncrementViaSequence 连续插入两条，验证 ID 递增（证明序列工作）
func TestAutoIncrementViaSequence(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	// 连续插入两条，验证 ID 递增（证明序列工作）
	u1 := User{Name: "Seq 1", Email: "seq1@example.com", Age: 1}
	u2 := User{Name: "Seq 2", Email: "seq2@example.com", Age: 2}
	if err := DB.Create(&u1).Error; err != nil {
		t.Fatalf("failed to create u1: %v", err)
	}
	if err := DB.Create(&u2).Error; err != nil {
		t.Fatalf("failed to create u2: %v", err)
	}

	if u1.ID == 0 {
		t.Error("expected u1.ID to be set")
	}
	if u2.ID != u1.ID+1 {
		t.Errorf("expected u2.ID = u1.ID+1, got u1.ID=%d, u2.ID=%d", u1.ID, u2.ID)
	}
}

// TestExplicitSequenceDefaultValue 验证：插入时不给主键 id，主键由序列自动生成。
//
// ⚠️ 重要发现：Oracle 11g 的 CREATE TABLE 的 DEFAULT 子句不允许引用序列的
// NEXTVAL（ORA-00984: 列在此处不允许，报错位置指向 NEXTVAL），该能力 12c 才引入。
// 因此任务原始 SQL "id NUMBER(19) DEFAULT SEQ_TEST_SEQ_DEFAULT.NEXTVAL" 无法在 11g 执行。
// 本测试改用 11g 的标准做法——BEFORE INSERT 触发器（与 go-ora migrator 给 autoIncrement
// 表生成的触发器机制相同）在 id 为 NULL 时从序列取值，验证语义与"DEFAULT 序列"等价：
// 插入时不提供 id，主键由序列自动生成并严格递增。
//
// 驱动行为实测（GORM v1.31.2 + go-ora v2.9.0，真实 11g 库）：
//   - GORM 会把非 autoIncrement 的 int/uint 主键自动视为自增（schema.go:337-348：
//     设 AutoIncrement=true、HasDefaultValue=true，并加入 FieldsWithDefaultDBValue）。
//   - 因此 DB.Create 不会把 id 列显式写入 INSERT，而是使用 RETURNING 回填；
//     由于表上有 BEFORE INSERT 触发器从序列取值，RETURNING 返回的即序列生成值。
//   - 实测 DB.Create 后 m1.ID 被回填为序列值（START WITH 100 → 100），非 0。
func TestExplicitSequenceDefaultValue(t *testing.T) {
	// 先删除可能残留的对象（忽略错误：对象可能不存在）
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEFAULT")
	DB.Migrator().DropTable(&SeqDefaultModel{})

	// 创建序列
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_SEQ_DEFAULT START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	// 测试结束清理序列和表（DROP TABLE 会级联删除其上的触发器）
	defer func() {
		DB.Exec("DROP TABLE TEST_SEQ_DEFAULT")
		DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEFAULT")
	}()

	// 建表（11g 的 DEFAULT 子句不支持引用序列，改用普通列 + 触发器）
	if err := DB.Exec(`CREATE TABLE TEST_SEQ_DEFAULT (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// 创建 BEFORE INSERT 触发器：id 为 NULL 时从序列取值
	triggerSQL := `CREATE OR REPLACE TRIGGER TRG_TEST_SEQ_DEFAULT
BEFORE INSERT ON TEST_SEQ_DEFAULT
FOR EACH ROW
BEGIN
	IF :NEW.id IS NULL THEN
		SELECT SEQ_TEST_SEQ_DEFAULT.NEXTVAL INTO :NEW.id FROM DUAL;
	END IF;
END;`
	if err := DB.Exec(triggerSQL).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}

	// 插入时不给 id（依赖触发器从序列默认取值）
	m1 := SeqDefaultModel{Name: "Default Seq 1"}
	if err := DB.Create(&m1).Error; err != nil {
		t.Fatalf("failed to create m1: %v", err)
	}
	if m1.ID == 0 {
		t.Error("expected m1.ID to be set from sequence")
	}
	t.Logf("m1.ID 回填=%d", m1.ID)

	// 用数据库查询交叉验证实际存储的 ID 来自序列
	var id1 uint
	if err := DB.Raw("SELECT id FROM TEST_SEQ_DEFAULT WHERE name = ?", "Default Seq 1").Scan(&id1).Error; err != nil {
		t.Fatalf("failed to query id1: %v", err)
	}
	if id1 == 0 {
		t.Error("expected database id1 to be set from sequence")
	}
	if id1 != m1.ID {
		t.Errorf("database id1=%d != m1.ID=%d", id1, m1.ID)
	}
	t.Logf("数据库实际 id=%d（序列 START WITH 100）", id1)

	// 第二次插入验证序列递增
	m2 := SeqDefaultModel{Name: "Default Seq 2"}
	if err := DB.Create(&m2).Error; err != nil {
		t.Fatalf("failed to create m2: %v", err)
	}
	if m2.ID != m1.ID+1 {
		t.Errorf("expected m2.ID = m1.ID+1, got m1.ID=%d, m2.ID=%d", m1.ID, m2.ID)
	}

	// 数据库端二次确认
	var id2 uint
	if err := DB.Raw("SELECT id FROM TEST_SEQ_DEFAULT WHERE name = ?", "Default Seq 2").Scan(&id2).Error; err != nil {
		t.Fatalf("failed to query id2: %v", err)
	}
	if id2 != id1+1 {
		t.Errorf("expected database id2 = id1+1, got id1=%d, id2=%d", id1, id2)
	}
	t.Logf("m2.ID 回填=%d, 数据库实际 id=%d", m2.ID, id2)
}

// TestSequenceDefaultViaAutoMigrate 验证 11g 下通过驱动 AutoMigrate 建表时的序列默认值触发器路径。
//
// 模型使用 gorm:"default:SEQ_TEST_SEQ_DEF_CODE.NEXTVAL"：
//   - 11g 的 CREATE TABLE 的 DEFAULT 子句不允许引用序列 NEXTVAL（ORA-00984），
//     驱动重写的 FullDataTypeOf 会跳过 DEFAULT 子句，建表后由 CreateTable 流程创建
//     BEFORE INSERT 触发器 SEQDEF_TRG_TEST_SEQ_DEF_CODE 实现等价语义；
//   - 12c+ 则不创建触发器，直接生成 DEFAULT SEQ_TEST_SEQ_DEF_CODE.NEXTVAL。
func TestSequenceDefaultViaAutoMigrate(t *testing.T) {
	// 先清理可能残留的对象（忽略错误：对象可能不存在）
	DB.Migrator().DropTable(&SeqDefaultViaDriverModel{})
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF_CODE")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF")
	DB.Exec("DROP TRIGGER SEQDEF_TRG_TEST_SEQ_DEF_CODE")

	// 手动创建序列：DEFAULT 引用的序列不会自动创建（autoIncrement 只负责创建
	// ID 用的 SEQ_TEST_SEQ_DEF），测试中先 DROP 再 CREATE，测试后清理。
	if err := DB.Exec("CREATE SEQUENCE SEQ_TEST_SEQ_DEF_CODE START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	// 测试结束清理表（DROP TABLE 会级联删除其上触发器）和序列
	defer func() {
		DB.Exec("DROP TABLE TEST_SEQ_DEF")
		DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF_CODE")
		DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF")
	}()

	// 通过驱动 AutoMigrate 建表（11g 下不会生成 DEFAULT <seq>.NEXTVAL 子句）
	if err := DB.AutoMigrate(&SeqDefaultViaDriverModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// 验证序列默认值触发器已创建（命名 SEQDEF_TRG_<table>_<column>，
	// 避免与 autoIncrement 的 TRG_TEST_SEQ_DEF 冲突）
	var trigCount int64
	if err := DB.Raw("SELECT COUNT(*) FROM USER_TRIGGERS WHERE TRIGGER_NAME = ?", "SEQDEF_TRG_TEST_SEQ_DEF_CODE").Scan(&trigCount).Error; err != nil {
		t.Fatalf("failed to query trigger: %v", err)
	}
	if trigCount == 0 {
		t.Error("expected sequence default trigger SEQDEF_TRG_TEST_SEQ_DEF_CODE to exist")
	}

	// 插入时不给 Code（字段在 FieldsWithDefaultDBValue 中，GORM 省略该列），
	// 触发器应从序列回填，RETURNING 将序列值回填到 m1.Code
	m1 := SeqDefaultViaDriverModel{Name: "Via AutoMigrate 1"}
	if err := DB.Create(&m1).Error; err != nil {
		t.Fatalf("failed to create m1: %v", err)
	}
	if m1.Code == 0 {
		t.Error("expected m1.Code to be set by sequence default trigger")
	}
	t.Logf("m1.Code 回填=%d（序列 START WITH 100）", m1.Code)

	// 查询数据库交叉验证 code 由触发器回填
	var code1 int
	if err := DB.Raw("SELECT code FROM TEST_SEQ_DEF WHERE name = ?", "Via AutoMigrate 1").Scan(&code1).Error; err != nil {
		t.Fatalf("failed to query code1: %v", err)
	}
	if code1 != m1.Code {
		t.Errorf("database code1=%d != m1.Code=%d", code1, m1.Code)
	}

	// 再次插入验证序列递增
	m2 := SeqDefaultViaDriverModel{Name: "Via AutoMigrate 2"}
	if err := DB.Create(&m2).Error; err != nil {
		t.Fatalf("failed to create m2: %v", err)
	}
	if m2.Code != m1.Code+1 {
		t.Errorf("expected m2.Code = m1.Code+1, got m1.Code=%d, m2.Code=%d", m1.Code, m2.Code)
	}
	t.Logf("m2.Code 回填=%d", m2.Code)
}
