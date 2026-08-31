package oracle

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"reflect"

	"gorm.io/gorm/schema"
)

// OracleGobSerializer 是 Oracle 优化的 GobSerializer
// 支持从 string 类型（VARCHAR2 列）解码 GOB 数据
type OracleGobSerializer struct{}

// Scan 实现 serializer interface
// 支持从 []byte 和 string 类型解码
func (OracleGobSerializer) Scan(ctx context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) (err error) {
	fieldValue := reflect.New(field.FieldType)

	if dbValue != nil {
		var bytesValue []byte
		switch v := dbValue.(type) {
		case []byte:
			bytesValue = v
		case string:
			// Oracle VARCHAR2 列返回 string 类型
			// 优先尝试 hex 解码（go-ora 可能返回十六进制编码的字符串）
			if decoded, err := hex.DecodeString(v); err == nil {
				bytesValue = decoded
			} else {
				// 如果 hex 解码失败，直接转换为 []byte
				// 这适用于直接存储的二进制数据
				bytesValue = []byte(v)
			}
		default:
			return fmt.Errorf("failed to unmarshal gob value: %#v", dbValue)
		}
		if len(bytesValue) > 0 {
			decoder := gob.NewDecoder(bytes.NewBuffer(bytesValue))
			err = decoder.Decode(fieldValue.Interface())
		}
	}
	field.ReflectValueOf(ctx, dst).Set(fieldValue.Elem())
	return
}

// Value 实现 serializer interface
func (OracleGobSerializer) Value(ctx context.Context, field *schema.Field, dst reflect.Value, fieldValue interface{}) (interface{}, error) {
	buf := new(bytes.Buffer)
	err := gob.NewEncoder(buf).Encode(fieldValue)
	return buf.Bytes(), err
}

func init() {
	// 注册 Oracle 优化的 GobSerializer
	// 这会覆盖 GORM 默认的 GobSerializer
	schema.RegisterSerializer("gob", OracleGobSerializer{})
}
