package oracle

import (
	"testing"

	"gorm.io/gorm/schema"
)

// TestDataTypeOf_VECTOR 验证 VECTOR 类型映射
func TestDataTypeOf_VECTOR(t *testing.T) {
	// 测试 23ai 版本下 VECTOR 类型的映射
	d := newTestDialector("23.0.0.0.0", 1024)

	field := &schema.Field{
		DataType: "vector",
		Size:     1536, // 向量维度
	}

	sqlType := d.DataTypeOf(field)
	expected := "VECTOR(1536)"

	if sqlType != expected {
		t.Errorf("DataTypeOf(VECTOR) = %q, expected %q", sqlType, expected)
	}
}

// TestDataTypeOf_VECTOR_OlderVersion 验证旧版本不支持 VECTOR
func TestDataTypeOf_VECTOR_OlderVersion(t *testing.T) {
	// 测试 21c 版本下 VECTOR 类型应报错或降级
	d := newTestDialector("21.0.0.0.0", 1024)

	field := &schema.Field{
		DataType: "vector",
		Size:     1536,
	}

	sqlType := d.DataTypeOf(field)
	// 21c 不支持 VECTOR，应返回错误或使用 CLOB 降级
	// 预期返回空字符串或错误提示
	if sqlType != "" {
		t.Errorf("DataTypeOf(VECTOR) on 21c should return empty string, got %q", sqlType)
	}
}

// TestDataTypeOf_VECTOR_DefaultDimension 测试默认维度
func TestDataTypeOf_VECTOR_DefaultDimension(t *testing.T) {
	d := newTestDialector("23.0.0.0.0", 1024)

	field := &schema.Field{
		DataType: "vector",
		Size:     0, // 未指定维度，应使用默认值
	}

	sqlType := d.DataTypeOf(field)
	expected := "VECTOR(1536)" // 默认维度

	if sqlType != expected {
		t.Errorf("DataTypeOf(VECTOR) with size=0 = %q, expected %q", sqlType, expected)
	}
}
