package driver_adapter

import (
	"context"
	"reflect"
	"testing"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
)

// invalidDSN 用于无需真实连接的场景：
// 空字符串在 go-ora 的 dsn 解析阶段即返回错误，避免触发真实 TCP 连接导致测试挂起
const invalidDSN = ""

// newTestAdapter 创建 GoOraAdapter 测试实例
func newTestAdapter() *GoOraAdapter {
	return &GoOraAdapter{}
}

// TestGoOraAdapterBasics 验证适配器基本属性
func TestGoOraAdapterBasics(t *testing.T) {
	a := newTestAdapter()

	if got := a.Name(); got != "go-ora" {
		t.Errorf("Name() = %q，期望 %q", got, "go-ora")
	}
	if got := a.Type(); got != DriverGoOra {
		t.Errorf("Type() = %q，期望 %q", got, DriverGoOra)
	}
	if !a.NeedsSizeForOut() {
		t.Error("NeedsSizeForOut() = false，期望 true")
	}
	if a.SupportsReturningMultiRow() {
		t.Error("SupportsReturningMultiRow() = true，期望 false")
	}
	if !a.SupportsBulkCopy() {
		t.Error("SupportsBulkCopy() = false，期望 true")
	}
}

// TestGoOraCreateOutParam 验证输出参数创建
func TestGoOraCreateOutParam(t *testing.T) {
	a := newTestAdapter()
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

	if _, ok := out.(*goOraOutParam); !ok {
		t.Errorf("返回类型 = %T，期望 *goOraOutParam", out)
	}
}

// TestGoOraCreateClob 验证 CLOB 创建
func TestGoOraCreateClob(t *testing.T) {
	a := newTestAdapter()

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
	if _, ok := lob.(*goOraLobData); !ok {
		t.Errorf("返回类型 = %T，期望 *goOraLobData", lob)
	}
}

// TestGoOraCreateBlob 验证 BLOB 创建
func TestGoOraCreateBlob(t *testing.T) {
	a := newTestAdapter()
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
	if _, ok := lob.(*goOraLobData); !ok {
		t.Errorf("返回类型 = %T，期望 *goOraLobData", lob)
	}
}

// TestGoOraCreateBatch 验证批量数据创建
func TestGoOraCreateBatch(t *testing.T) {
	a := newTestAdapter()
	want := []interface{}{1, "a"}

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
	if _, ok := batch.(*goOraBatchData); !ok {
		t.Errorf("返回类型 = %T，期望 *goOraBatchData", batch)
	}
}

// TestGoOraWrapForInsert 验证插入包装
func TestGoOraWrapForInsert(t *testing.T) {
	a := newTestAdapter()

	// CLOB 包装
	clob, ok := a.WrapClobForInsert("text").(go_ora.Clob)
	if !ok {
		t.Fatalf("WrapClobForInsert 返回类型 = %T，期望 go_ora.Clob", a.WrapClobForInsert("text"))
	}
	if clob.String != "text" {
		t.Errorf("Clob.String = %q，期望 %q", clob.String, "text")
	}
	if !clob.Valid {
		t.Error("Clob.Valid = false，期望 true")
	}

	// BLOB 包装
	blob, ok := a.WrapBlobForInsert([]byte{1}).(go_ora.Blob)
	if !ok {
		t.Fatalf("WrapBlobForInsert 返回类型 = %T，期望 go_ora.Blob", a.WrapBlobForInsert([]byte{1}))
	}
	if !reflect.DeepEqual(blob.Data, []byte{1}) {
		t.Errorf("Blob.Data = %v，期望 [1]", blob.Data)
	}
	if !blob.Valid {
		t.Error("Blob.Valid = false，期望 true")
	}
}

// TestGoOraOpen 验证 Open 只检查驱动名注册，不会真正建立连接
func TestGoOraOpen(t *testing.T) {
	a := newTestAdapter()

	db, err := a.Open("oracle://user:pass@localhost:1521/service")
	if err != nil {
		// sql.Open 只在驱动未注册时报错；go-ora 包的 init 已注册 "oracle" 驱动名
		t.Fatalf("Open() 返回错误: %v（需要 go-ora 包 init 注册 \"oracle\" 驱动名）", err)
	}
	if db == nil {
		t.Fatal("Open() 返回 db == nil")
	}
	defer db.Close()
}

// TestGoOraPing 验证对未连接数据库的 Ping 返回错误而非 panic
func TestGoOraPing(t *testing.T) {
	a := newTestAdapter()

	db, err := a.Open(invalidDSN)
	if err != nil {
		t.Fatalf("Open() 返回错误: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := a.Ping(ctx, db); err == nil {
		t.Error("Ping(未连接数据库) 返回 nil，期望返回错误")
	}
}

// TestGoOraUnwrapQueryResult 验证查询结果原样返回
func TestGoOraUnwrapQueryResult(t *testing.T) {
	a := newTestAdapter()

	cases := []interface{}{
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

// TestGoOraGetConnection 验证获取底层连接不 panic（未连接数据库时返回错误即可）
func TestGoOraGetConnection(t *testing.T) {
	a := newTestAdapter()

	db, err := a.Open(invalidDSN)
	if err != nil {
		t.Fatalf("Open() 返回错误: %v", err)
	}
	defer db.Close()

	raw, err := a.GetConnection(db)
	if err != nil {
		t.Logf("GetConnection 返回预期错误（未连接数据库）: %v", err)
		return
	}
	if raw == nil {
		t.Error("GetConnection 返回 nil raw 且无错误")
	}
}
