package oracle

import (
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
)

// 以下测试用枚举类型仅定义于本测试文件（package oracle 根目录下无同名类型，
// tests/ 包中的同名类型属于独立包，互不影响）。
type MerchantStatus int
type MerchantName string
type MerchantBool bool
type merchantUint uint
type merchantFloat float64

// TestCheckEnumNamedValue 测试 checkEnumNamedValue 对各种值类型的判定：
// 命名基本类型（Go 枚举）应返回 true（触发 ErrSkip 委托默认转换），
// 裸 driver.Value 应返回 false（由 go-ora 直接处理），
// 特殊类型有明确的预期行为。
func TestCheckEnumNamedValue(t *testing.T) {
	t.Run("命名 int 枚举返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: MerchantStatus(5)}
		if !checkEnumNamedValue(nv) {
			t.Error("MerchantStatus 应返回 true（委托默认转换）")
		}
	})

	t.Run("命名 string 枚举返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: MerchantName("x")}
		if !checkEnumNamedValue(nv) {
			t.Error("MerchantName 应返回 true")
		}
	})

	t.Run("命名 bool 枚举返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: MerchantBool(true)}
		if !checkEnumNamedValue(nv) {
			t.Error("MerchantBool 应返回 true")
		}
	})

	t.Run("命名 uint 枚举返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: merchantUint(7)}
		if !checkEnumNamedValue(nv) {
			t.Error("merchantUint 应返回 true")
		}
	})

	t.Run("命名 float 枚举返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: merchantFloat(3.5)}
		if !checkEnumNamedValue(nv) {
			t.Error("merchantFloat 应返回 true")
		}
	})

	t.Run("裸 int64 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: int64(5)}
		if checkEnumNamedValue(nv) {
			t.Error("裸 int64 是 driver.Value，应返回 false")
		}
	})

	t.Run("裸 string 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: "hello"}
		if checkEnumNamedValue(nv) {
			t.Error("裸 string 是 driver.Value，应返回 false")
		}
	})

	t.Run("裸 bool 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: true}
		if checkEnumNamedValue(nv) {
			t.Error("裸 bool 是 driver.Value，应返回 false")
		}
	})

	t.Run("driver.Valuer（sql.NullString）返回 true", func(t *testing.T) {
		nv := &driver.NamedValue{Value: sql.NullString{String: "x", Valid: true}}
		if !checkEnumNamedValue(nv) {
			t.Error("driver.Valuer 应返回 true（委托默认转换调用 Value()）")
		}
	})

	t.Run("go_ora.Out 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: go_ora.Out{Dest: new(string)}}
		if checkEnumNamedValue(nv) {
			t.Error("go_ora.Out（struct）应返回 false，交由 go-ora 自己的 checker")
		}
	})

	t.Run("time.Time 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: time.Time{}}
		if checkEnumNamedValue(nv) {
			t.Error("time.Time 是 driver.Value，应返回 false")
		}
	})

	t.Run("[]byte 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: []byte("x")}
		if checkEnumNamedValue(nv) {
			t.Error("[]byte 是 driver.Value，应返回 false")
		}
	})

	t.Run("nil 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: nil}
		if checkEnumNamedValue(nv) {
			t.Error("nil 应返回 false")
		}
	})

	t.Run("nil 指针返回 false", func(t *testing.T) {
		var p *MerchantName
		nv := &driver.NamedValue{Value: p}
		if checkEnumNamedValue(nv) {
			t.Error("nil 指针应返回 false")
		}
	})

	t.Run("struct 返回 false", func(t *testing.T) {
		nv := &driver.NamedValue{Value: struct{ A int }{1}}
		if checkEnumNamedValue(nv) {
			t.Error("未实现 driver.Valuer 的 struct 应返回 false")
		}
	})

	t.Run("指针包裹的命名枚举返回 true", func(t *testing.T) {
		p := MerchantName("x")
		nv := &driver.NamedValue{Value: &p}
		if !checkEnumNamedValue(nv) {
			t.Error("*MerchantName 应返回 true（解指针后为 string 命名类型）")
		}
	})
}

// tEnum 仅用于本测试文件的枚举类型（package oracle 根目录下无同名类型）。
type tEnum int

// mockNormalizeStmt 最小 mock driver.Stmt：实现 Stmt 必需方法与
// NamedValueChecker，CheckNamedValue 记录收到的值供断言。
type mockNormalizeStmt struct {
	lastValue any
}

func (m *mockNormalizeStmt) Close() error  { return nil }
func (m *mockNormalizeStmt) NumInput() int { return 0 }
func (m *mockNormalizeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, nil
}
func (m *mockNormalizeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, nil
}
func (m *mockNormalizeStmt) CheckNamedValue(nv *driver.NamedValue) error {
	m.lastValue = nv.Value
	return nil
}

// TestEnumNormalizeStmtCheckNamedValue 验证 prepared 路径（Stmt 级 NamedValueChecker）
// 下枚举参数委托 ErrSkip：enumNormalizeStmt.CheckNamedValue 对命名枚举返回
// driver.ErrSkip（不改写 nv.Value），对 Valuer 同样返回 ErrSkip，对裸值转发
// 给内部 Stmt。
func TestEnumNormalizeStmtCheckNamedValue(t *testing.T) {
	inner := &mockNormalizeStmt{}
	stmt := &enumNormalizeStmt{inner: inner}

	t.Run("命名枚举返回 ErrSkip 且不改写值", func(t *testing.T) {
		nv := &driver.NamedValue{Ordinal: 1, Value: tEnum(7)}
		err := stmt.CheckNamedValue(nv)
		if err != driver.ErrSkip {
			t.Fatalf("期望 driver.ErrSkip，实际 %v", err)
		}
		// checkEnumNamedValue 不改写 nv.Value，只是判断后返回 ErrSkip
		if nv.Value != tEnum(7) {
			t.Errorf("nv.Value 不应被改写，实际 %#v (%T)", nv.Value, nv.Value)
		}
		// 命中 ErrSkip 短路，不转发给内部 checker
		if inner.lastValue != nil {
			t.Errorf("命中 ErrSkip 时不应转发给内部 checker，实际 mock 收到 %#v", inner.lastValue)
		}
	})

	t.Run("driver.Valuer 返回 ErrSkip", func(t *testing.T) {
		want := sql.NullString{Valid: true, String: "x"}
		nv := &driver.NamedValue{Ordinal: 2, Value: want}
		err := stmt.CheckNamedValue(nv)
		if err != driver.ErrSkip {
			t.Fatalf("期望 driver.ErrSkip，实际 %v", err)
		}
		// nv.Value 保持原样（checkEnumNamedValue 不改写）
		if _, ok := nv.Value.(sql.NullString); !ok {
			t.Errorf("Valuer 值不应被改写，实际 %#v (%T)", nv.Value, nv.Value)
		}
		// Valuer 命中 ErrSkip，不转发
		if inner.lastValue != nil {
			t.Errorf("Valuer 命中 ErrSkip 时不应转发给内部 checker，实际 mock 收到 %#v", inner.lastValue)
		}
	})

	t.Run("裸 int64 转发给内部 checker 并返回 nil", func(t *testing.T) {
		nv := &driver.NamedValue{Ordinal: 3, Value: int64(42)}
		err := stmt.CheckNamedValue(nv)
		if err != nil {
			t.Fatalf("期望 nil，实际 %v", err)
		}
		// 裸值不命中 checkEnumNamedValue，转发给内部 checker
		if inner.lastValue != int64(42) {
			t.Errorf("mock 应收到 int64(42)，实际 %#v", inner.lastValue)
		}
	})
}
