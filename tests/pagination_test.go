package tests

import (
	"fmt"
	"testing"
)

// TestPagination 真库分页回归：验证分页三种形态的行数与边界语义
// （11g 走 RewriteLimit11 的 ROW_NUMBER/BETWEEN/ROWNUM 重写，12c+ 走 OFFSET/FETCH，
// 本测试只断言行数与首尾行内容，对两种路径均适用）。
// 数据为固定 20 行，Name 编码行序：pag_00 为第 1 行，pag_19 为第 20 行。
func TestPagination(t *testing.T) {
	// 独立建表，测试结束清理；不改动 main_test.go 的共享 cleanup
	_ = DB.Migrator().DropTable(&User{})
	if err := DB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("failed to migrate TEST_USERS: %v", err)
	}
	t.Cleanup(func() {
		_ = DB.Migrator().DropTable(&User{})
	})

	const total = 20
	users := make([]User, 0, total)
	for i := range total {
		users = append(users, User{
			Name:  fmt.Sprintf("pag_%02d", i),
			Email: fmt.Sprintf("pag_%02d@oracle.test", i),
			Age:   i + 1,
		})
	}
	if err := DB.CreateInBatches(&users, 10).Error; err != nil {
		t.Fatalf("failed to seed %d rows: %v", total, err)
	}

	t.Run("offset only returns rows after offset", func(t *testing.T) {
		// Offset(10) 无 Limit：应跳过前 10 行，返回第 11~20 行，恰好 10 行
		var got []User
		if err := DB.Order("id").Offset(10).Find(&got).Error; err != nil {
			t.Fatalf("Offset(10) query failed: %v", err)
		}
		if len(got) != 10 {
			t.Fatalf("Offset(10) returned %d rows, want 10", len(got))
		}
		if got[0].Name != "pag_10" {
			t.Errorf("Offset(10) first row = %q, want pag_10 (第 11 条)", got[0].Name)
		}
		if got[len(got)-1].Name != "pag_19" {
			t.Errorf("Offset(10) last row = %q, want pag_19", got[len(got)-1].Name)
		}
	})

	t.Run("limit with offset", func(t *testing.T) {
		// Offset(5).Limit(4)：应返回第 6~9 行，恰好 4 行
		var got []User
		if err := DB.Order("id").Offset(5).Limit(4).Find(&got).Error; err != nil {
			t.Fatalf("Offset(5).Limit(4) query failed: %v", err)
		}
		if len(got) != 4 {
			t.Fatalf("Offset(5).Limit(4) returned %d rows, want 4", len(got))
		}
		if got[0].Name != "pag_05" {
			t.Errorf("Offset(5).Limit(4) first row = %q, want pag_05 (第 6 条)", got[0].Name)
		}
		if got[len(got)-1].Name != "pag_08" {
			t.Errorf("Offset(5).Limit(4) last row = %q, want pag_08", got[len(got)-1].Name)
		}
	})

	t.Run("limit only", func(t *testing.T) {
		// Limit(3) 无 Offset：应返回前 3 行
		var got []User
		if err := DB.Order("id").Limit(3).Find(&got).Error; err != nil {
			t.Fatalf("Limit(3) query failed: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("Limit(3) returned %d rows, want 3", len(got))
		}
		if got[0].Name != "pag_00" {
			t.Errorf("Limit(3) first row = %q, want pag_00", got[0].Name)
		}
	})
}
