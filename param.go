package oracle

import (
	"github.com/sijms/go-ora/v2"
)

// Param 是存储过程参数的本地包装结构
// 用于隐藏底层驱动（当前为 go-ora）的细节，提供统一的 API
// godror 为路线图项（未接线），暂不涉及切换
type Param struct {
	// Dest 是参数的目标值（用于 OUT/IN OUT 参数）
	Dest interface{}
	// Size 是字符串类型参数的大小（用于 VARCHAR2 等变长类型）
	Size int
	// In 表示是否为 IN 或 IN OUT 参数
	// false: OUT 参数
	// true: IN 或 IN OUT 参数
	In bool
}

// OutParam 创建一个 OUT 参数
// 用于存储过程的输出参数
func OutParam(dest interface{}, size int) Param {
	return Param{
		Dest: dest,
		Size: size,
		In:   false,
	}
}

// InOutParam 创建一个 IN OUT 参数
// 用于存储过程的输入输出参数
func InOutParam(dest interface{}) Param {
	return Param{
		Dest: dest,
		In:   true,
	}
}

// InParam 创建一个 IN 参数（通常不需要，因为普通参数就是 IN 参数）
// 此函数主要用于明确语义
func InParam(value interface{}) interface{} {
	return value
}

// ToDriverParam 将本地 Param 转换为底层驱动的 Out 结构
// 此函数在驱动层使用，业务代码不应直接调用
func ToDriverParam(p Param) go_ora.Out {
	return go_ora.Out{
		Dest: p.Dest,
		Size: p.Size,
		In:   p.In,
	}
}

// RefCursor 是游标类型的本地包装
// 用于存储过程返回结果集
type RefCursor struct {
	cursor go_ora.RefCursor
}

// NewRefCursor 创建一个新的游标参数
func NewRefCursor() *RefCursor {
	return &RefCursor{}
}

// ToDriverCursor 将本地 RefCursor 转换为底层驱动的 RefCursor
func (r *RefCursor) ToDriverCursor() *go_ora.RefCursor {
	return &r.cursor
}
