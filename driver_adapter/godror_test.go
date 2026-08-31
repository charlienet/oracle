//go:build godror

package driver_adapter

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// newTestGodrorAdapter 创建 GodrorAdapter 测试实例
func newTestGodrorAdapter() *GodrorAdapter {
	return &GodrorAdapter{}
}

// TestGodrorAdapterBasics 验证适配器基本属性
func TestGodrorAdapterBasics(t *testing.T) {
	a := newTestGodrorAdapter()

	if got := a.Name(); got != "godror" {
		t.Errorf("Name() = %q，期望 %q", got, "godror")
	}
	if got := a.Type(); got != DriverGodror {
		t.Errorf("Type() = %q，期望 %q", got, DriverGodror)
	}
	if a.NeedsSizeForOut() {
		t.Error("NeedsSizeForOut() = true，期望 false")
	}
	if !a.SupportsReturningMultiRow() {
		t.Error("SupportsReturningMultiRow() = false，期望 true")
	}
	if a.SupportsBulkCopy() {
		t.Error("SupportsBulkCopy() = true，期望 false")
	}
}

// TestGodrorCreateOutParam 验证输出参数创建
func TestGodrorCreateOutParam(t *testing.T) {
	a := newTestGodrorAdapter()
	var id int

	out := a.CreateOutParam(&id, 100)
	if out == nil {
		t.Fatal("CreateOutParam 返回 nil")
	}
	if got := out.GetDest(); got != &id {
		t.Errorf("GetDest() = %v，期望 %v", got, &id)
	}
	if got := out.GetSize(); got != 100 {
		t.Errorf("GetSize() = %d，期望 100", got)
	}

	out.SetSize(200)
	if got := out.GetSize(); got != 200 {
		t.Errorf("SetSize 后 GetSize() = %d，期望 200", got)
	}

	if _, ok := out.(*godrorOutParam); !ok {
		t.Errorf("返回类型 = %T，期望 *godrorOutParam", out)
	}
}

// TestGodrorCreateClob 验证 CLOB 创建
func TestGodrorCreateClob(t *testing.T) {
	a := newTestGodrorAdapter()

	lob := a.CreateClob("text")
	if lob == nil {
		t.Fatal("CreateClob 返回 nil")
	}
	if !lob.IsCLOB() {
		t.Error("IsCLOB() = false，期望 true")
	}
	if lob.IsBLOB() {
		t.Error("IsBLOB() = true，期望 false")
	}
	if got := lob.GetString(); got != "text" {
		t.Errorf("GetString() = %q，期望 %q", got, "text")
	}
	if !lob.IsValid() {
		t.Error("IsValid() = false，期望 true")
	}
	if _, ok := lob.(*godrorLobData); !ok {
		t.Errorf("返回类型 = %T，期望 *godrorLobData", lob)
	}
}

// TestGodrorCreateBlob 验证 BLOB 创建
func TestGodrorCreateBlob(t *testing.T) {
	a := newTestGodrorAdapter()
	want := []byte{1, 2, 3}

	lob := a.CreateBlob(want)
	if lob == nil {
		t.Fatal("CreateBlob 返回 nil")
	}
	if !lob.IsBLOB() {
		t.Error("IsBLOB() = false，期望 true")
	}
	if lob.IsCLOB() {
		t.Error("IsCLOB() = true，期望 false")
	}
	if got := lob.GetBytes(); !reflect.DeepEqual(got, want) {
		t.Errorf("GetBytes() = %v，期望 %v", got, want)
	}
	if !lob.IsValid() {
		t.Error("IsValid() = false，期望 true")
	}
	if _, ok := lob.(*godrorLobData); !ok {
		t.Errorf("返回类型 = %T，期望 *godrorLobData", lob)
	}
}

// TestGodrorCreateBatch 验证批量数据创建
func TestGodrorCreateBatch(t *testing.T) {
	a := newTestGodrorAdapter()
	want := []any{1, "a"}

	batch := a.CreateBatch(want)
	if batch == nil {
		t.Fatal("CreateBatch 返回 nil")
	}
	if got := batch.Len(); got != 2 {
		t.Errorf("Len() = %d，期望 2", got)
	}
	got := batch.GetValues()
	if len(got) != len(want) {
		t.Fatalf("GetValues() 长度 = %d，期望 %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("GetValues()[%d] = %v，期望 %v", i, got[i], want[i])
		}
	}
	if _, ok := batch.(*godrorBatchData); !ok {
		t.Errorf("返回类型 = %T，期望 *godrorBatchData", batch)
	}
}

// TestGodrorWrapForInsert 验证插入包装
func TestGodrorWrapForInsert(t *testing.T) {
	a := newTestGodrorAdapter()

	// CLOB 包装（godror 直接返回字符串）
	clob := a.WrapClobForInsert("text")
	if got, ok := clob.(string); !ok || got != "text" {
		t.Errorf("WrapClobForInsert 返回 = %v (类型 %T)，期望字符串 \"text\"", clob, clob)
	}

	// BLOB 包装（godror 直接返回字节数组）
	blob := a.WrapBlobForInsert([]byte{1})
	if got, ok := blob.([]byte); !ok || !reflect.DeepEqual(got, []byte{1}) {
		t.Errorf("WrapBlobForInsert 返回 = %v (类型 %T)，期望 []byte{1}", blob, blob)
	}
}

// TestGodrorOpen 验证 Open 只检查驱动名注册，不会真正建立连接
func TestGodrorOpen(t *testing.T) {
	a := newTestGodrorAdapter()

	// 使用空的 DSN，因为 godror 在打开时会立即尝试连接
	// 我们只验证 Open 不会 panic 并返回 *sql.DB
	db, err := a.Open("")
	if err != nil {
		// 如果没有安装 godror 驱动，这里会返回错误
		t.Logf("Open() 返回错误（可能未安装 godror 驱动）: %v", err)
		return
	}
	if db == nil {
		t.Fatal("Open() 返回 db == nil")
	}
	defer func() { _ = db.Close() }()
}

// TestGodrorPing 验证对未连接数据库的 Ping 返回错误而非 panic
func TestGodrorPing(t *testing.T) {
	a := newTestGodrorAdapter()

	db, err := a.Open("")
	if err != nil {
		t.Logf("Open() 返回错误（可能未安装 godror 驱动）: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := a.Ping(ctx, db); err == nil {
		t.Error("Ping(未连接数据库) 返回 nil，期望返回错误")
	} else {
		t.Logf("Ping(未连接数据库) 返回预期错误: %v", err)
	}
}

// TestGodrorUnwrapQueryResult 验证查询结果解包
func TestGodrorUnwrapQueryResult(t *testing.T) {
	a := newTestGodrorAdapter()

	// 测试指针类型解包
	str := "hello"
	ptrStr := &str
	if got := a.UnwrapQueryResult(ptrStr, ""); got != str {
		t.Errorf("UnwrapQueryResult(*string) = %v，期望 %v", got, str)
	}

	// 测试 nil 指针
	var nilStr *string
	if got := a.UnwrapQueryResult(nilStr, ""); got != nil {
		t.Errorf("UnwrapQueryResult(nil *string) = %v，期望 nil", got)
	}

	// 测试字节切片指针
	bytes := []byte{1, 2, 3}
	ptrBytes := &bytes
	if got := a.UnwrapQueryResult(ptrBytes, ""); !reflect.DeepEqual(got, bytes) {
		t.Errorf("UnwrapQueryResult(*[]byte) = %v，期望 %v", got, bytes)
	}

	// 测试 nil 字节切片指针
	var nilBytes *[]byte
	if got := a.UnwrapQueryResult(nilBytes, ""); got != nil {
		t.Errorf("UnwrapQueryResult(nil *[]byte) = %v，期望 nil", got)
	}

	// 测试普通类型原样返回
	cases := []any{
		42,
		"hello",
		[]byte{1, 2, 3},
		3.14,
		nil,
	}
	for _, in := range cases {
		if got := a.UnwrapQueryResult(in, ""); !reflect.DeepEqual(got, in) {
			t.Errorf("UnwrapQueryResult(%v, \"\") = %v，期望原样返回", in, got)
		}
	}
}

// TestGodrorGetConnection 验证获取底层连接不 panic
func TestGodrorGetConnection(t *testing.T) {
	a := newTestGodrorAdapter()

	db, err := a.Open("")
	if err != nil {
		t.Logf("Open() 返回错误（可能未安装 godror 驱动）: %v", err)
		return
	}
	defer func() { _ = db.Close() }()

	raw, err := a.GetConnection(db)
	if err != nil {
		t.Logf("GetConnection 返回预期错误（未连接数据库）: %v", err)
		return
	}
	if raw == nil {
		t.Error("GetConnection 返回 nil raw 且无错误")
	}
}
