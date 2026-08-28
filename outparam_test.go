package oracle

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"testing"

	go_ora "github.com/sijms/go-ora/v2"
	"gorm.io/gorm/schema"
)

// ---- 测试用枚举类型（本文件内定义） ----
type testStatus int
type testCode string
type testFlag bool

func TestOutParam_BareInt64(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.Int,
		FieldType:   reflect.TypeFor[int64](),
		Size:        64,
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	dest, ok := out.Dest.(*int64)
	if !ok {
		t.Fatalf("expected *int64, got %T", out.Dest)
	}
	_ = dest
	if out.Size != 0 {
		t.Errorf("int64 size should be 0 (定长), got %d", out.Size)
	}
}

func TestOutParam_BareString(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.String,
		FieldType:   reflect.TypeFor[string](),
		Size:        100,
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	dest, ok := out.Dest.(*string)
	if !ok {
		t.Fatalf("expected *string, got %T", out.Dest)
	}
	_ = dest
	if out.Size != 100 {
		t.Errorf("string size should be 100, got %d", out.Size)
	}
}

func TestOutParam_BareBool(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.Bool,
		FieldType:   reflect.TypeFor[bool](),
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	dest, ok := out.Dest.(*bool)
	if !ok {
		t.Fatalf("expected *bool, got %T", out.Dest)
	}
	_ = dest
}

func TestOutParam_NamedEnumInt(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.Int,
		FieldType:   reflect.TypeFor[testStatus](),
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	// testStatus（命名 int）→ bareType 归一为 *int64
	dest, ok := out.Dest.(*int64)
	if !ok {
		t.Fatalf("expected *int64 for testStatus enum, got %T", out.Dest)
	}
	_ = dest
}

func TestOutParam_NamedEnumString(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.String,
		FieldType:   reflect.TypeFor[testCode](),
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	dest, ok := out.Dest.(*string)
	if !ok {
		t.Fatalf("expected *string for testCode enum, got %T", out.Dest)
	}
	_ = dest
}

func TestOutParam_NamedEnumBool(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.Bool,
		FieldType:   reflect.TypeFor[testFlag](),
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	dest, ok := out.Dest.(*bool)
	if !ok {
		t.Fatalf("expected *bool for testFlag enum, got %T", out.Dest)
	}
	_ = dest
}

func TestOutParam_VarcharWithSize(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.String,
		FieldType:   reflect.TypeFor[string](),
		Size:        256,
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	if out.Size != 256 {
		t.Errorf("expected size=256, got %d", out.Size)
	}
}

func TestOutParam_StringNoSize(t *testing.T) {
	// Size=0 的字符串字段应回退到 4000
	f := &schema.Field{
		DataType:    schema.String,
		FieldType:   reflect.TypeFor[string](),
		Size:        0,
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	if out.Size != 4000 {
		t.Errorf("expected size=4000 (default), got %d", out.Size)
	}
}

func TestOutParamSize(t *testing.T) {
	tests := []struct {
		name     string
		field    *schema.Field
		expected int
	}{
		{"string with size", &schema.Field{DataType: schema.String, Size: 100}, 100},
		{"string no size → 4000", &schema.Field{DataType: schema.String, Size: 0}, 4000},
		{"bytes with size", &schema.Field{DataType: schema.Bytes, Size: 512}, 512},
		{"bytes no size → 4000", &schema.Field{DataType: schema.Bytes, Size: 0}, 4000},
		{"text → 4000", &schema.Field{DataType: "text", Size: 0}, 4000},
		{"json → 4000", &schema.Field{DataType: "json", Size: 0}, 4000},
		{"int → 0", &schema.Field{DataType: schema.Int, Size: 0}, 0},
		{"bool → 0", &schema.Field{DataType: schema.Bool, Size: 0}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := outParamSize(tt.field)
			if got != tt.expected {
				t.Errorf("outParamSize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestOutParam_Structure(t *testing.T) {
	f := &schema.Field{
		DataType:    schema.String,
		FieldType:   reflect.TypeFor[string](),
		Size:        200,
		TagSettings: map[string]string{},
	}
	out := outParam(f)
	// outParam 返回 go_ora.Out（值类型），用指针接收后验证字段
	op, ok := any(&out).(*go_ora.Out)
	if !ok {
		t.Fatalf("expected *go_ora.Out via pointer, got %T", out)
	}
	if op.Size != 200 {
		t.Errorf("expected Size=200, got %d", op.Size)
	}
}

func TestOutParamOutDest(t *testing.T) {
	strVal := "hello"
	outVal := go_ora.Out{Dest: &strVal}
	vars := []any{42, outVal, "skip"}

	got := outDest(vars, 1)
	if _, ok := got.(*string); !ok {
		t.Fatalf("expected *string, got %T", got)
	}
}

// TestIsBareKind 确认 isBareKind 覆盖所有基本类型
func TestIsBareKind(t *testing.T) {
	bareKinds := []reflect.Kind{
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Bool, reflect.String,
	}
	for _, k := range bareKinds {
		if !isBareKind(k) {
			t.Errorf("isBareKind(%v) = false, want true", k)
		}
	}

	nonBareKinds := []reflect.Kind{
		reflect.Struct, reflect.Slice, reflect.Pointer, reflect.Interface, reflect.Map,
	}
	for _, k := range nonBareKinds {
		if isBareKind(k) {
			t.Errorf("isBareKind(%v) = true, want false", k)
		}
	}
}

// TestCheckEnumNamedValueExported 确保 checkEnumNamedValue 在 package 内可访问
func TestCheckEnumNamedValueExported(t *testing.T) {
	// 命名枚举类型
	nv := &driver.NamedValue{Value: testStatus(1)}
	if !checkEnumNamedValue(nv) {
		t.Error("testStatus should return true")
	}

	// 裸值
	nv2 := &driver.NamedValue{Value: int64(1)}
	if checkEnumNamedValue(nv2) {
		t.Error("int64 should return false")
	}

	// sql.NullString (driver.Valuer)
	nv3 := &driver.NamedValue{Value: sql.NullString{String: "x", Valid: true}}
	if !checkEnumNamedValue(nv3) {
		t.Error("sql.NullString should return true")
	}
}
