package tests

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

// changedHookModel 带 BeforeUpdate 钩子，验证 tx.Statement.Changed 在钩子内的行为。
// ChangedName/ChangedAny 为内存标记（gorm:"-" 不建列），由钩子在更新前写入。
type changedHookModel struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:100"`
	Age         int
	ChangedName bool `gorm:"-"`
	ChangedAny  bool `gorm:"-"`
}

func (changedHookModel) TableName() string {
	return "TEST_CHANGED_HOOK"
}

// BeforeUpdate 记录本次更新涉及的字段变化，供断言验证 Statement.Changed
//
// 注意：map 键需用 Go 字段名（"Name"/"Age"）。本驱动 Namer 将 DBName 大写化
// （"NAME"/"AGE"），GORM Changed 的 map 分支匹配 mv[field.Name]（字段名）或
// mv[field.DBName]（DBName），用小写 DBName 键（"name"）两者都无法命中。
func (m *changedHookModel) BeforeUpdate(tx *gorm.DB) error {
	m.ChangedName = tx.Statement.Changed("name")
	m.ChangedAny = tx.Statement.Changed()
	return nil
}

func TestUpdateExprIntegration(t *testing.T) {
	// 表达式更新：price = price + 1.5，验证 gorm.Expr 走表达式路径而非绑定参数
	if err := DB.AutoMigrate(&Product{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_PRODUCTS")

	p := Product{Name: "expr-test", Price: 10.5}
	if err := DB.Create(&p).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	if err := DB.Model(&Product{}).Where("id = ?", p.ID).Update("price", gorm.Expr("price + ?", 1.5)).Error; err != nil {
		t.Fatalf("failed to update with expr: %v", err)
	}

	var got Product
	if err := DB.First(&got, p.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Price != 12.0 {
		t.Errorf("表达式更新后 Price 期望 12.0，实际 %v", got.Price)
	}
}

func TestUpdateSubQueryIntegration(t *testing.T) {
	// 子查询更新：u1.age = (SELECT age FROM TEST_USERS WHERE id = u2.ID)
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	u1 := User{Name: "sub1", Email: "sub1@example.com", Age: 5}
	u2 := User{Name: "sub2", Email: "sub2@example.com", Age: 10}
	if err := DB.Create(&u1).Error; err != nil {
		t.Fatalf("failed to create u1: %v", err)
	}
	if err := DB.Create(&u2).Error; err != nil {
		t.Fatalf("failed to create u2: %v", err)
	}

	if err := DB.Model(&User{}).Where("id = ?", u1.ID).Update("age", DB.Model(&User{}).Select("age").Where("id = ?", u2.ID)).Error; err != nil {
		t.Fatalf("failed to update with subquery: %v", err)
	}

	var got User
	if err := DB.First(&got, u1.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Age != u2.Age {
		t.Errorf("子查询更新后 u1.age 期望 %d，实际 %d", u2.Age, got.Age)
	}
}

func TestUpdateOmitIntegration(t *testing.T) {
	// Omit("name") + Updates(struct)：name 保持旧值，其余字段正常更新
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	u := User{Name: "旧名", Email: "omit@example.com", Age: 1}
	if err := DB.Create(&u).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	if err := DB.Model(&User{}).Where("id = ?", u.ID).Omit("name").Updates(User{Name: "新名", Age: 99}).Error; err != nil {
		t.Fatalf("failed to update with omit: %v", err)
	}

	var got User
	if err := DB.First(&got, u.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Age != 99 {
		t.Errorf("Omit 更新后 age 期望 99，实际 %d", got.Age)
	}
	if got.Name != "旧名" {
		t.Errorf("Omit(\"name\") 后 name 应保持旧名，实际 %q", got.Name)
	}
}

func TestUpdateChangedHook(t *testing.T) {
	// BeforeUpdate 钩子内 Statement.Changed 应准确反映本次更新涉及的字段
	if err := DB.AutoMigrate(&changedHookModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_CHANGED_HOOK")

	m := &changedHookModel{Name: "x1", Age: 1}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	// 修改 name：Changed("name") 与 Changed() 均应为 true
	// （map 键用 Go 字段名 "Name"，见 BeforeUpdate 注释）
	if err := DB.Model(&m).Updates(map[string]any{"Name": "x2"}).Error; err != nil {
		t.Fatalf("failed to update name: %v", err)
	}
	if !m.ChangedName {
		t.Errorf("修改 name 后 Changed(\"name\") 应为 true，实际 false")
	}
	if !m.ChangedAny {
		t.Errorf("修改 name 后 Changed() 应为 true，实际 false")
	}

	// 只改 age：Changed("name") 应为 false，Changed() 应为 true（age 已变化）
	if err := DB.Model(&m).Updates(map[string]any{"Age": 2}).Error; err != nil {
		t.Fatalf("failed to update age: %v", err)
	}
	if m.ChangedName {
		t.Errorf("只改 age 时 Changed(\"name\") 应为 false，实际 true")
	}
	if !m.ChangedAny {
		t.Errorf("只改 age 时 Changed() 应为 true（age 已变化），实际 false")
	}
}

// TestUpdateInTransactionCommitAndRollback 验证事务内组合更新的提交与回滚一致性：
// 提交后落库值正确；事务内返回错误后回滚，回读为旧值。
func TestUpdateInTransactionCommitAndRollback(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	u := User{Name: "tx1", Email: "tx1@example.com", Age: 1}
	if err := DB.Create(&u).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	// 事务 A：组合更新（表达式 + 子查询 + Omit struct）后提交
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("age", gorm.Expr("age + ?", 1)).Error; err != nil {
			return err
		}
		sub := tx.Model(&User{}).Select("age").Where("id = ?", u.ID)
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("age", sub).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Omit("email").Updates(User{Name: "tx1-提交", Age: 42}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务 A 提交失败: %v", err)
	}

	var got User
	if err := DB.First(&got, u.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Age != 42 {
		t.Errorf("事务 A 提交后 age 期望 42，实际 %d", got.Age)
	}
	if got.Name != "tx1-提交" {
		t.Errorf("事务 A 提交后 name 期望 tx1-提交，实际 %q", got.Name)
	}

	// 事务 B：更新后返回错误，应整体回滚
	before := got.Age
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("age", 99).Error; err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("事务 B 期望返回错误，实际 nil")
	}
	if err := DB.First(&got, u.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Age != before {
		t.Errorf("事务 B 回滚后 age 应保持 %d，实际 %d", before, got.Age)
	}
}

// TestUpdateSelfManagedTxRollbackOnError 验证非事务路径下的自管事务回滚：
// 利用 User.Email 唯一索引构造 ORA-00001 冲突，断言错误上抛且数据未变
// （覆盖 ensureWriteTx 的 *sql.DB 自管事务回滚分支）。
func TestUpdateSelfManagedTxRollbackOnError(t *testing.T) {
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	u1 := User{Name: "u1", Email: "u1@example.com", Age: 1}
	u2 := User{Name: "u2", Email: "u2@example.com", Age: 2}
	if err := DB.Create(&u1).Error; err != nil {
		t.Fatalf("failed to create u1: %v", err)
	}
	if err := DB.Create(&u2).Error; err != nil {
		t.Fatalf("failed to create u2: %v", err)
	}

	err := DB.Model(&User{}).Where("id = ?", u1.ID).Update("email", u2.Email).Error
	if err == nil {
		t.Fatal("期望唯一约束冲突错误（ORA-00001），实际 nil")
	}

	var got User
	if err := DB.First(&got, u1.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Email != "u1@example.com" {
		t.Errorf("冲突失败后 email 应保持 u1@example.com，实际 %q", got.Email)
	}
}

// TestPrepareStmtUpdateInTransactionRollback 验证 prepared 事务
// （PreparedStmtTX 分支）下 Update 的回滚路径：事务内更新后返回错误，
// 回读应保持旧值。
func TestPrepareStmtUpdateInTransactionRollback(t *testing.T) {
	db := openPreparedDB(t)
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_USERS")

	u := User{Name: "prep-tx", Email: "prep-tx@example.com", Age: 7}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", u.ID).Update("age", 99).Error; err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("prepared 事务期望返回错误，实际 nil")
	}

	var got User
	if err := db.First(&got, u.ID).Error; err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if got.Age != 7 {
		t.Errorf("prepared 事务回滚后 age 应保持 7，实际 %d", got.Age)
	}
}
