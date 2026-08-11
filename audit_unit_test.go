package oracle

import (
	"reflect"
	"testing"
)

// TestClauseBuildersInvalidVersion 复现：DBVer 字符串解析失败（dbver=0）时，
// ClauseBuilders 应保守使用 11g 的 ROWNUM 方案，而不是 12c+ 的 FETCH NEXT
// 语法（否则 11g 下 LIMIT/OFFSET 查询报 ORA-00933）。
func TestClauseBuildersInvalidVersion(t *testing.T) {
	d := newTestDialector("invalid-version", 2000)
	got := reflect.ValueOf(d.ClauseBuilders()["LIMIT"]).Pointer()
	want := reflect.ValueOf(d.RewriteLimit11).Pointer()
	if got != want {
		t.Errorf("invalid DBVer: LIMIT builder should be RewriteLimit11, got %v want %v", got, want)
	}

	// 正常 11g 版本也应走 RewriteLimit11
	d11 := newTestDialector("11.2.0.4.0", 2000)
	if p := reflect.ValueOf(d11.ClauseBuilders()["LIMIT"]).Pointer(); p != reflect.ValueOf(d11.RewriteLimit11).Pointer() {
		t.Error("11g DBVer should use RewriteLimit11")
	}

	// 12c+ 走 RewriteLimit
	d19 := newTestDialector("19.0.0.0", 2000)
	if p := reflect.ValueOf(d19.ClauseBuilders()["LIMIT"]).Pointer(); p != reflect.ValueOf(d19.RewriteLimit).Pointer() {
		t.Error("19c DBVer should use RewriteLimit")
	}
}
