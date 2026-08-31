package oracle

import (
	"bytes"
	"testing"

	go_ora "github.com/sijms/go-ora/v2"
	"gorm.io/gorm/schema"
)

// TestConvertFromOracleToField_Clob 验证 CLOB 类型转换
func TestConvertFromOracleToField_Clob(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected any
	}{
		{
			name:     "有效 CLOB",
			value:    go_ora.Clob{String: "test data", Valid: true},
			expected: "test data",
		},
		{
			name:     "无效 CLOB",
			value:    go_ora.Clob{String: "", Valid: false},
			expected: nil,
		},
		{
			name:     "空字符串 CLOB",
			value:    go_ora.Clob{String: "", Valid: true},
			expected: "",
		},
		{
			name:     "JSON 字符串 CLOB",
			value:    go_ora.Clob{String: `{"key":"value"}`, Valid: true},
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFromOracleToField(tt.value, nil)
			if result != tt.expected {
				t.Errorf("convertFromOracleToField(%v) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

// TestConvertFromOracleToField_Blob 验证 BLOB 类型转换
func TestConvertFromOracleToField_Blob(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected any
	}{
		{
			name:     "有效 BLOB",
			value:    go_ora.Blob{Data: []byte{1, 2, 3, 4}, Valid: true},
			expected: []byte{1, 2, 3, 4},
		},
		{
			name:     "无效 BLOB",
			value:    go_ora.Blob{Data: nil, Valid: false},
			expected: nil,
		},
		{
			name:     "空字节切片 BLOB",
			value:    go_ora.Blob{Data: []byte{}, Valid: true},
			expected: []byte{},
		},
		{
			name:     "GOB 编码数据 BLOB",
			value:    go_ora.Blob{Data: []byte{0x0f, 0xff, 0x00}, Valid: true},
			expected: []byte{0x0f, 0xff, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertFromOracleToField(tt.value, nil)
			if result == nil && tt.expected != nil {
				t.Errorf("convertFromOracleToField(%v) = nil, want %v", tt.value, tt.expected)
				return
			}
			if result != nil && tt.expected == nil {
				t.Errorf("convertFromOracleToField(%v) = %v, want nil", tt.value, result)
				return
			}
			// 比较字节切片
			if resultBytes, ok := result.([]byte); ok {
				if expectedBytes, ok := tt.expected.([]byte); ok {
					if len(resultBytes) != len(expectedBytes) {
						t.Errorf("长度不匹配: got %d, want %d", len(resultBytes), len(expectedBytes))
						return
					}
					for i := range resultBytes {
						if resultBytes[i] != expectedBytes[i] {
							t.Errorf("字节 %d 不匹配: got %d, want %d", i, resultBytes[i], expectedBytes[i])
							return
						}
					}
				} else {
					t.Errorf("类型不匹配: got []byte, want %T", tt.expected)
				}
			}
		})
	}
}

// TestConvertFromOracleToField_PointerLob 验证指针类型的 LOB 转换
func TestConvertFromOracleToField_PointerLob(t *testing.T) {
	t.Run("指针 CLOB", func(t *testing.T) {
		clob := &go_ora.Clob{String: "pointer test", Valid: true}
		result := convertFromOracleToField(clob, nil)
		if result != "pointer test" {
			t.Errorf("convertFromOracleToField(%v) = %v, want 'pointer test'", clob, result)
		}
	})

	t.Run("指针 BLOB", func(t *testing.T) {
		blob := &go_ora.Blob{Data: []byte{5, 6, 7}, Valid: true}
		result := convertFromOracleToField(blob, nil)
		if resultBytes, ok := result.([]byte); !ok || len(resultBytes) != 3 {
			t.Errorf("convertFromOracleToField(%v) = %v, want []byte{5,6,7}", blob, result)
		}
	})

	t.Run("nil 指针 CLOB", func(t *testing.T) {
		var clob *go_ora.Clob = nil
		result := convertFromOracleToField(clob, nil)
		if result != nil {
			t.Errorf("convertFromOracleToField(nil Clob) = %v, want nil", result)
		}
	})
}

// TestConvertFromOracleToField_SerializerIntegration 验证 serializer 字段的完整转换
func TestConvertFromOracleToField_SerializerIntegration(t *testing.T) {
	t.Run("JSON serializer 字段从 CLOB 读取", func(t *testing.T) {
		// 模拟从 Oracle CLOB 列读取 JSON 数据
		clob := go_ora.Clob{
			String: `{"name":"test","age":30}`,
			Valid:  true,
		}

		// 转换应该返回字符串，GORM 的 serializer 会将其反序列化为 map
		result := convertFromOracleToField(clob, nil)
		if result != `{"name":"test","age":30}` {
			t.Errorf("期望 JSON 字符串，实际 %v", result)
		}
	})

	t.Run("GOB serializer 字段从 BLOB 读取", func(t *testing.T) {
		// 模拟从 Oracle BLOB 列读取 GOB 数据
		blob := go_ora.Blob{
			Data:  []byte{0x0f, 0xff, 0x81, 0x04, 0x02}, // 模拟 GOB 编码数据
			Valid: true,
		}

		// 转换应该返回 []byte，GORM 的 gob serializer 会将其解码
		result := convertFromOracleToField(blob, nil)
		if resultBytes, ok := result.([]byte); !ok || len(resultBytes) != 5 {
			t.Errorf("期望 []byte{0x0f, 0xff, 0x81, 0x04, 0x02}，实际 %v", result)
		}
	})

	t.Run("GOB serializer 字段从十六进制字符串读取", func(t *testing.T) {
		// 模拟 go-ora 在某些情况下返回十六进制编码的 BLOB 字符串
		// 例如：错误信息中的 "0D7F040102FF8000010C0104000015FF800003036F6E65020374776F0405746872656506"
		hexString := "0D7F040102"

		field := &schema.Field{
			DataType: schema.Bytes,
			TagSettings: map[string]string{
				"SERIALIZER": "gob",
			},
		}

		// 转换应该将十六进制字符串解码为 []byte
		result := convertFromOracleToField(hexString, field)
		if resultBytes, ok := result.([]byte); !ok {
			t.Errorf("期望 []byte，实际 %T", result)
		} else if len(resultBytes) != 5 {
			t.Errorf("期望长度 5，实际 %d", len(resultBytes))
		} else {
			expected := []byte{0x0D, 0x7F, 0x04, 0x01, 0x02}
			for i, b := range resultBytes {
				if b != expected[i] {
					t.Errorf("字节 %d 不匹配: 期望 %02x, 实际 %02x", i, expected[i], b)
				}
			}
		}
	})

	t.Run("非 Bytes 类型字段不解码十六进制字符串", func(t *testing.T) {
		// 非 []byte 字段的十六进制字符串应该原样返回
		hexString := "0D7F040102"

		field := &schema.Field{
			DataType: schema.String,
		}

		result := convertFromOracleToField(hexString, field)
		if result != hexString {
			t.Errorf("期望原样返回字符串，实际 %v", result)
		}
	})

	t.Run("无效十六进制字符串按原始字节返回", func(t *testing.T) {
		// 无效 hex 不应被解码，按原始字节 []byte 返回
		invalidHex := "ZZZZ"

		field := &schema.Field{
			DataType: schema.Bytes,
		}

		result := convertFromOracleToField(invalidHex, field)
		resultBytes, ok := result.([]byte)
		if !ok || !bytes.Equal(resultBytes, []byte("ZZZZ")) {
			t.Errorf("期望 []byte(\"ZZZZ\")，实际 %v", result)
		}
	})

	t.Run("unixtime serializer 字段从 NUMBER 读取", func(t *testing.T) {
		// unixtime 存储为 NUMBER，go-ora 可能返回 string 或 int64
		// 这里测试 string 路径（go-ora 对 NUMBER 列的常见行为）
		var timestamp int64 = 1705315800 // 2024-01-15 10:30:00 UTC

		field := &schema.Field{
			DataType: schema.Int,
			TagSettings: map[string]string{
				"SERIALIZER": "unixtime",
			},
		}

		// NUMBER 列通常不需要特殊转换，但这里验证兼容性
		result := convertFromOracleToField(timestamp, field)
		if result != int64(1705315800) {
			t.Errorf("期望 int64(1705315800)，实际 %v", result)
		}
	})
}
