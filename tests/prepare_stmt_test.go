package tests

import (
	"errors"
	"os"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	oracle "github.com/charlienet/oracle"
)

// 本文件验证 C-1 缺陷修复：PrepareStmt 模式下写回调（Create/Update/Delete）
// 的正确性与原子性。
//
// 背景：PrepareStmt: true 时 stmt.ConnPool 为 *gorm.PreparedStmtDB（连接池）或
// *gorm.PreparedStmtTX（事务），旧实现的 *sql.Tx / Begin()(*sql.Tx, error) 断言
// 均落空，所有写操作报 "unsupported connection pool type"。
// 修复后由 ensureWriteTx 按池类型分派：必要时包自管事务保持批量原子性。

// openPreparedDB 打开独立的 PrepareStmt 连接（与 main_test.go 的默认 DB 并行使用）。
// DSN 经 ORACLE_DSN 环境变量注入，避免凭据落盘。
func openPreparedDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN 未设置，跳过 PrepareStmt 集成测试")
	}
	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{PrepareStmt: true})
	if err != nil {
		t.Fatalf("failed to connect database (PrepareStmt): %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// migrateAndClear 为指定实例迁移并清空表（t.Cleanup 保证结束时删表）
func migrateAndClear(t *testing.T, db *gorm.DB, model any, table string) {
	t.Helper()
	if err := db.AutoMigrate(model); err != nil {
		t.Fatalf("failed to migrate %s: %v", table, err)
	}
	if err := db.Exec("DELETE FROM " + table).Error; err != nil {
		t.Fatalf("failed to clear table %s: %v", table, err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(model)
	})
}

// R1：PrepareStmt 下单行 Create，主键回填与行数正确
func TestPrepareStmtCreateSingle(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	user := User{Name: "ps-single", Email: "ps-single@example.com", Age: 30, Active: true}
	result := db.Create(&user)
	if result.Error != nil {
		t.Fatalf("PrepareStmt 单行 Create 失败: %v", result.Error)
	}
	if user.ID == 0 {
		t.Error("期望 Create 后主键 ID 被回填，实际为 0")
	}
	if result.RowsAffected != 1 {
		t.Errorf("期望受影响行数 1，实际 %d", result.RowsAffected)
	}
}

// R2：PrepareStmt 下批量 Create（逐行执行），行数与回填正确
func TestPrepareStmtCreateBatch(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	users := []User{
		{Name: "ps-batch-1", Email: "ps-b1@example.com", Age: 20},
		{Name: "ps-batch-2", Email: "ps-b2@example.com", Age: 21},
		{Name: "ps-batch-3", Email: "ps-b3@example.com", Age: 22},
	}
	result := db.Create(&users)
	if result.Error != nil {
		t.Fatalf("PrepareStmt 批量 Create 失败: %v", result.Error)
	}
	if result.RowsAffected != 3 {
		t.Errorf("期望受影响行数 3，实际 %d", result.RowsAffected)
	}
	for i, u := range users {
		if u.ID == 0 {
			t.Errorf("第 %d 行主键 ID 未回填", i)
		}
	}
}

// R3（合并门槛用例）：PrepareStmt 下含默认值字段的 Create，
// RETURNING INTO 的 Out 参数在 prepared 语句上正确取回。
// Order.Status 带默认值 'pending'（字符串、Size>0），创建时不赋值，断言回填生效。
func TestPrepareStmtCreateWithDefaultValues(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &Order{}, "TEST_ORDERS")

	// 先建前置 User，满足 Order.UserID 的外键约束
	owner := User{Name: "ps-default-owner", Email: "ps-default-owner@example.com", Age: 40}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("创建前置 User 失败: %v", err)
	}

	order := Order{
		UserID: owner.ID,
		Total:  99.5,
		// Status 不赋值：GORM 对 default 字段省略该列，INSERT 携带
		// RETURNING status INTO :out，DB 默认值 'pending' 经 Out 参数回填到结构体
	}
	// 回填路径验证：Out 参数（Size=20，prepared 语句）取回 DB 默认值
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("PrepareStmt 带默认值字段 Create 失败: %v", err)
	}
	if order.ID == 0 {
		t.Fatal("期望主键 ID 被回填，实际为 0")
	}
	if order.Status != "pending" {
		t.Errorf("RETURNING 回填的 Status 期望 pending，实际 %q", order.Status)
	}

	// 落库值一致性验证
	var order2 Order
	if err := db.First(&order2, order.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if order2.Status != "pending" {
		t.Errorf("Status 落库值期望 pending，实际 %q", order2.Status)
	}
}

// R4（合并门槛用例）：PrepareStmt 批量中途失败（撞唯一索引）→
// 报错且表内 0 行，验证自管事务的原子性回滚。
func TestPrepareStmtCreateBatchAtomicity(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	dup := "ps-dup@example.com"
	users := []User{
		{Name: "atomic-1", Email: "atomic-1@example.com", Age: 20},
		{Name: "atomic-2", Email: dup, Age: 21},
		{Name: "atomic-3", Email: dup, Age: 22}, // 与第 2 行 Email 相同，撞唯一索引
	}
	result := db.Create(&users)
	if result.Error == nil {
		t.Fatal("批量撞唯一索引应返回错误，实际为 nil")
	}

	var count int64
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if count != 0 {
		t.Errorf("批量失败后应全量回滚（0 行），实际表内 %d 行", count)
	}
}

// R5：PrepareStmt 下 db.Transaction 内 Create，事务 Rollback 后 0 行。
// 事务内回调看到的连接池是事务池，ensureWriteTx 应原样使用且不重复提交。
func TestPrepareStmtCreateInTransaction(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	err := db.Transaction(func(tx *gorm.DB) error {
		u := User{Name: "ps-in-tx", Email: "ps-in-tx@example.com", Age: 25}
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		if u.ID == 0 {
			t.Error("事务内 Create 主键未回填")
		}
		return errors.New("主动回滚")
	})
	if err == nil {
		t.Fatal("事务应返回主动回滚的错误")
	}

	var count int64
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if count != 0 {
		t.Errorf("事务回滚后应 0 行，实际 %d 行", count)
	}
}

// R6-a：PrepareStmt 下 Update 正常执行
func TestPrepareStmtUpdate(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	u := User{Name: "ps-update", Email: "ps-update@example.com", Age: 30}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	result := db.Model(&User{}).Where("id = ?", u.ID).Update("name", "ps-updated")
	if result.Error != nil {
		t.Fatalf("PrepareStmt Update 失败: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Errorf("期望受影响行数 1，实际 %d", result.RowsAffected)
	}

	var got User
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Name != "ps-updated" {
		t.Errorf("更新后 Name 期望 ps-updated，实际 %q", got.Name)
	}
}

// R6-b：PrepareStmt 下硬删除（Unscoped）正常执行
func TestPrepareStmtHardDelete(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	u := User{Name: "ps-hard", Email: "ps-hard@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	result := db.Unscoped().Delete(&User{}, u.ID)
	if result.Error != nil {
		t.Fatalf("PrepareStmt 硬删除失败: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Errorf("期望受影响行数 1，实际 %d", result.RowsAffected)
	}

	var count int64
	db.Model(&User{}).Where("id = ?", u.ID).Count(&count)
	if count != 0 {
		t.Errorf("硬删除后应 0 行，实际 %d 行", count)
	}
}

// R6-c：PrepareStmt 下软删除正常执行（物理行保留，deleted_at 置值）
func TestPrepareStmtSoftDelete(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	u := User{Name: "ps-soft", Email: "ps-soft@example.com"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	result := db.Delete(&User{}, u.ID)
	if result.Error != nil {
		t.Fatalf("PrepareStmt 软删除失败: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Errorf("期望受影响行数 1，实际 %d", result.RowsAffected)
	}

	// 软删后默认查询不可见
	var count int64
	db.Model(&User{}).Where("id = ?", u.ID).Count(&count)
	if count != 0 {
		t.Errorf("软删除后默认查询应不可见，实际可见 %d 行", count)
	}
	// Unscoped 查询可见（物理行保留）
	var deleted User
	if err := db.Unscoped().First(&deleted, u.ID).Error; err != nil {
		t.Fatalf("软删除后 Unscoped 回读失败: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Error("软删除后 DeletedAt 应有效（非零值）")
	}
}

// R7：PrepareStmt 下单行 MERGE（OnConflict DoUpdates）生效
func TestPrepareStmtOnConflictMerge(t *testing.T) {
	db := openPreparedDB(t)
	migrateAndClear(t, db, &OnConflictTestModel{}, "TEST_ON_CONFLICT")

	// 初始记录
	initial := OnConflictTestModel{
		ApplyNumber: "PS-APP001",
		MerchantID:  "PS-MCH001",
		Name:        "Initial",
		Address:     "Addr-Initial",
	}
	if err := db.Create(&initial).Error; err != nil {
		t.Fatalf("创建初始记录失败: %v", err)
	}

	// 同主键再 Create，触发 MERGE WHEN MATCHED 更新
	conflict := OnConflictTestModel{
		ApplyNumber: "PS-APP001",
		MerchantID:  "PS-MCH001",
		Name:        "Merged",
		Address:     "Addr-Merged",
	}
	result := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "APPLY_NUMBER"},
			{Name: "MERCHANT_ID"},
		},
		DoUpdates: clause.Set{
			{Column: clause.Column{Name: "NAME"}, Value: conflict.Name},
			{Column: clause.Column{Name: "ADDRESS"}, Value: conflict.Address},
		},
	}).Create(&conflict)
	if result.Error != nil {
		t.Fatalf("PrepareStmt MERGE 失败: %v", result.Error)
	}

	var got OnConflictTestModel
	if err := db.Where("APPLY_NUMBER = ? AND MERCHANT_ID = ?", "PS-APP001", "PS-MCH001").First(&got).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Name != "Merged" || got.Address != "Addr-Merged" {
		t.Errorf("MERGE DoUpdates 未生效：Name=%q Address=%q", got.Name, got.Address)
	}
}

// R8：SkipDefaultTransaction: true 的独立 PrepareStmt 实例，
// 批量中途失败必须仍全量回滚——锁死 ensureWriteTx 自管事务的等价行为。
// 此场景下 gorm 不再自动包默认事务，stmt.ConnPool 为 *gorm.PreparedStmtDB，
// 原子性完全由驱动回调内的自管事务保证。
func TestSkipDefaultTransactionBatchAtomicity(t *testing.T) {
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		t.Skip("ORACLE_DSN 未设置，跳过 PrepareStmt 集成测试")
	}
	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
		PrepareStmt:            true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	migrateAndClear(t, db, &User{}, "TEST_USERS")

	dup := "skip-dup@example.com"
	users := []User{
		{Name: "skip-1", Email: "skip-1@example.com", Age: 20},
		{Name: "skip-2", Email: dup, Age: 21},
		{Name: "skip-3", Email: dup, Age: 22},
	}
	result := db.Create(&users)
	if result.Error == nil {
		t.Fatal("批量撞唯一索引应返回错误，实际为 nil")
	}

	var count int64
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if count != 0 {
		t.Errorf("SkipDefaultTransaction 下批量失败应全量回滚（0 行），实际 %d 行", count)
	}
}
