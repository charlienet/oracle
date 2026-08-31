package oracle

import (
	"testing"

	"gorm.io/gorm/schema"
)

// TestDataTypeOf_SerializerJSON 验证 DataTypeOf 对 JSON serializer 字段的类型映射
func TestDataTypeOf_SerializerJSON(t *testing.T) {
	d := Dialector{Config: &Config{DBVer: "19.0.0.0.0"}}

	// JSON serializer 字段应映射为 VARCHAR2 或 CLOB（取决于 size）
	field := &schema.Field{
		DataType: schema.String,
		Size:     1000,
		TagSettings: map[string]string{
			"SERIALIZER": "json",
		},
	}

	sqlType := d.DataTypeOf(field)
	t.Logf("JSON serializer 字段（size=1000）的类型: %s", sqlType)

	// 应该是 VARCHAR2(1000)，而不是其他类型
	if sqlType != "VARCHAR2(1000)" {
		t.Errorf("期望 VARCHAR2(1000), 实际 %s", sqlType)
	}
}

// TestDataTypeOf_SerializerJSON_CLOB 验证大 JSON 字段的类型映射
func TestDataTypeOf_SerializerJSON_CLOB(t *testing.T) {
	// 探测到 MAX_STRING_SIZE=EXTENDED 时 32k VARCHAR2 生效，size=5000 映射为 VARCHAR2(5000)；
	// 未探测/STANDARD 时上限 4000，应映射为 CLOB（见 TestDataTypeOfFullMatrix 的 STANDARD/未探测用例）
	d := Dialector{Config: &Config{DBVer: "19.0.0.0.0", MaxStringSize: MaxStringSizeExtended}}

	// 大 JSON 字段（size > 2000）应映射为 CLOB
	field := &schema.Field{
		DataType: schema.String,
		Size:     5000,
		TagSettings: map[string]string{
			"SERIALIZER": "json",
		},
	}

	sqlType := d.DataTypeOf(field)
	t.Logf("JSON serializer 字段（size=5000）的类型: %s", sqlType)

	// 12c+ 应该是 VARCHAR2(5000)，11g 应该是 CLOB
	if sqlType != "VARCHAR2(5000)" {
		t.Errorf("期望 VARCHAR2(5000), 实际 %s", sqlType)
	}
}

// TestDataTypeOf_SerializerJSON_ExplicitCLOB 验证显式 CLOB 类型
func TestDataTypeOf_SerializerJSON_ExplicitCLOB(t *testing.T) {
	d := Dialector{Config: &Config{DBVer: "19.0.0.0.0"}}

	// 显式指定 type:clob 的字段
	field := &schema.Field{
		DataType: "clob",
		TagSettings: map[string]string{
			"SERIALIZER": "json",
			"TYPE":       "clob",
		},
	}

	sqlType := d.DataTypeOf(field)
	t.Logf("JSON serializer 字段（type=clob）的类型: %s", sqlType)

	if sqlType != "CLOB" {
		t.Errorf("期望 CLOB, 实际 %s", sqlType)
	}
}

// TestDataTypeOf_SerializerUnixTime 验证 unixtime serializer 字段的类型映射
// gorm 的 UnixSecondSerializer 语义：int64 字段（Unix 秒）序列化为 time.Time 存储
// （Value 返回 time.Time、Scan 经 sql.NullTime 接收），列型须为 TIMESTAMP 系列，
// 而非 int64 默认的 INTEGER/NUMBER（否则写入 ORA-00932、读取 Scan 失败）。
func TestDataTypeOf_SerializerUnixTime(t *testing.T) {
	d := Dialector{Config: &Config{DBVer: "19.0.0.0.0"}}

	// unixtime serializer 字段（Go 类型 int64 → schema.Int）
	field := &schema.Field{
		DataType: schema.Int,
		Size:     64, // int64
		TagSettings: map[string]string{
			"SERIALIZER": "unixtime",
		},
	}

	sqlType := d.DataTypeOf(field)
	t.Logf("unixtime serializer 字段的类型: %s", sqlType)

	// 应为 TIMESTAMP WITH TIME ZONE（与 schema.Time 一致），而非 INTEGER
	if sqlType != "TIMESTAMP WITH TIME ZONE" {
		t.Errorf("期望 TIMESTAMP WITH TIME ZONE, 实际 %s", sqlType)
	}

	// 回归：11g 下同样为 TIMESTAMP（unixtime 不依赖版本特性）
	d11 := Dialector{Config: &Config{DBVer: "11.2.0.4.0"}}
	if got := d11.DataTypeOf(field); got != "TIMESTAMP WITH TIME ZONE" {
		t.Errorf("11g 下期望 TIMESTAMP WITH TIME ZONE, 实际 %s", got)
	}

	// 回归：无 serializer 的普通 int64 仍按 INTEGER（unixtime 分支不影响既有映射）
	plain := &schema.Field{
		DataType:    schema.Int,
		Size:        64,
		TagSettings: map[string]string{},
	}
	if got := d.DataTypeOf(plain); got != "INTEGER" {
		t.Errorf("普通 int64 期望 INTEGER, 实际 %s", got)
	}

	// 回归：普通 time.Time 无 serializer 仍按 TIMESTAMP WITH TIME ZONE
	timeField := &schema.Field{
		DataType:    schema.Time,
		TagSettings: map[string]string{},
	}
	if got := d.DataTypeOf(timeField); got != "TIMESTAMP WITH TIME ZONE" {
		t.Errorf("普通 time.Time 期望 TIMESTAMP WITH TIME ZONE, 实际 %s", got)
	}
}

// TestDataTypeOf_SerializerGob 验证 gob serializer 字段的类型映射
func TestDataTypeOf_SerializerGob(t *testing.T) {
	d := Dialector{Config: &Config{DBVer: "19.0.0.0.0"}}

	// gob serializer 将字段序列化为字节（[]byte）
	// GORM 会将 DataType 设置为 schema.Bytes
	field := &schema.Field{
		DataType: schema.Bytes,
		TagSettings: map[string]string{
			"SERIALIZER": "gob",
		},
	}

	sqlType := d.DataTypeOf(field)
	t.Logf("gob serializer 字段的类型: %s", sqlType)

	// 应该是 BLOB
	if sqlType != "BLOB" {
		t.Errorf("期望 BLOB, 实际 %s", sqlType)
	}
}
