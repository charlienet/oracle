package tests

// 本文件固化「事务隔离级别」限制（负向测试锁定驱动行为，防未来驱动版本变化时静默失效）：
//
// go-ora v2.9.0 的 Connection.BeginTx 对事务隔离级别有硬限制（connection.go:584-590）：
//   - opts.ReadOnly == true          → 报错 "readonly transaction is not supported"
//   - opts.Isolation != 0（任意显式值）→ 报错 "only support default value for isolation"
//
// 即仅「默认隔离级别」可用；Oracle 默认隔离级别即 READ COMMITTED，但**显式**传入
// sql.LevelReadCommitted（=2，非 0）同样会被 go-ora 拒绝。详见 LIMITATIONS.md
// 「驱动实现限制 → 5. 事务隔离级别」。

import (
	"database/sql"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestTransactionIsolation_Default 正向：默认隔离级别（Oracle 默认即 READ COMMITTED）
// 不传 TxOptions 走 driver.Conn.Begin 默认路径，事务内 INSERT 提交后可查回。
// 注：这是 go-ora 唯一支持的隔离级别形态（显式 Isolation 值一律被拒，见文件头注释）。
func TestTransactionIsolation_Default(t *testing.T) {
	type IsolationModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&IsolationModel{})
	if err := DB.AutoMigrate(&IsolationModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&IsolationModel{}) }()

	// 默认事务（READ COMMITTED 语义）
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return tx.Create(&IsolationModel{Name: "default-rc"}).Error
	}); err != nil {
		t.Fatalf("default transaction failed: %v", err)
	}

	// 事务内 INSERT 已提交，可查回
	var count int64
	if err := DB.Model(&IsolationModel{}).Where("name = ?", "default-rc").Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 record after default transaction commit, got %d", count)
	}
}

// TestTransactionIsolation_Unsupported 负向：非默认隔离级别不可用（go-ora 驱动限制）
// 断言：显式指定隔离级别（READ COMMITTED / SERIALIZABLE）时事务无法开始、明确报错。
// 此测试固化该限制：未来驱动若支持这些隔离级别，本测试将显式失败，提示重新评估
// （届时需同步更新 LIMITATIONS.md「驱动实现限制 → 5. 事务隔离级别」）。
func TestTransactionIsolation_Unsupported(t *testing.T) {
	type IsolationModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&IsolationModel{})
	if err := DB.AutoMigrate(&IsolationModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&IsolationModel{}) }()

	cases := []struct {
		name      string
		isolation sql.IsolationLevel
	}{
		// 注意：go-ora 仅接受默认值（Isolation==0），显式 READ COMMITTED（=2）同样被拒
		{"显式 READ COMMITTED", sql.LevelReadCommitted},
		{"SERIALIZABLE", sql.LevelSerializable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := DB.Transaction(func(tx *gorm.DB) error {
				// 事务应无法开始，不应执行到这里
				return tx.Create(&IsolationModel{Name: "should-not-commit"}).Error
			}, &sql.TxOptions{Isolation: tc.isolation})

			if err == nil {
				t.Fatalf("expected error for isolation %s (go-ora limitation), got nil", tc.isolation)
			}

			// 宽松断言：错误文本含隔离级别相关关键词之一（go-ora 固定报错文本为
			// "only support default value for isolation"，实测后可收紧为精确匹配）
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "isolation") &&
				!strings.Contains(msg, "not supported") &&
				!strings.Contains(msg, "ora-") {
				t.Errorf("expected error to mention isolation limitation, got: %v", err)
			}

			t.Logf("isolation %s 被拒绝（符合 go-ora 限制）: %v", tc.isolation, err)
		})
	}
}
