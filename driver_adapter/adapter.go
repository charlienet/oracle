// Package driver_adapter 提供 Oracle 驱动抽象层
// 支持 go-ora 和 godror 两种底层驱动的切换
package driver_adapter

import (
	"context"
	"database/sql"
	"sync"
)

// DriverType 驱动类型枚举
type DriverType string

const (
	// DriverGoOra 使用纯 Go 实现的 go-ora 驱动
	DriverGoOra DriverType = "go-ora"
	// DriverGodror 使用基于 ODPI-C 的 godror 驱动
	DriverGodror DriverType = "godror"
)

// OutParam 输出参数接口，用于 RETURNING INTO 子句
type OutParam interface {
	// GetDest 返回目标指针
	GetDest() any
	// SetSize 设置缓冲区大小（用于字符串类型）
	SetSize(size int)
	// GetSize 获取缓冲区大小
	GetSize() int
}

// LobData LOB 数据接口
type LobData interface {
	// IsCLOB 是否为 CLOB 类型
	IsCLOB() bool
	// IsBLOB 是否为 BLOB 类型
	IsBLOB() bool
	// GetString 获取字符串值（CLOB）
	GetString() string
	// GetBytes 获取字节值（BLOB）
	GetBytes() []byte
	// IsValid 是否有效
	IsValid() bool
}

// BatchData 批量数据接口
type BatchData interface {
	// Len 返回数据长度
	Len() int
	// GetValues 返回所有值
	GetValues() []any
}

// Adapter 驱动适配器接口
// 封装了不同 Oracle 驱动的差异，提供统一的 API
type Adapter interface {
	// Name 返回驱动名称
	Name() string

	// Type 返回驱动类型
	Type() DriverType

	// Open 打开数据库连接
	Open(dsn string) (*sql.DB, error)

	// CreateOutParam 创建输出参数（用于 RETURNING INTO）
	// dest: 目标指针
	// size: 缓冲区大小（字符串类型需要）
	CreateOutParam(dest any, size int) OutParam

	// CreateClob 创建 CLOB 数据
	CreateClob(value string) LobData

	// CreateBlob 创建 BLOB 数据
	CreateBlob(value []byte) LobData

	// CreateBatch 创建批量数据
	CreateBatch(values []any) BatchData

	// NeedsSizeForOut 返回输出参数是否需要指定 Size
	// go-ora 对字符串类型的 Out 参数需要指定 Size
	// godror 通常不需要
	NeedsSizeForOut() bool

	// SupportsReturningMultiRow 返回是否支持多行 RETURNING
	// go-ora 不支持批量 INSERT + RETURNING
	// godror 支持
	SupportsReturningMultiRow() bool

	// SupportsBulkCopy 返回是否支持 BulkCopy
	SupportsBulkCopy() bool

	// WrapClobForInsert 包装 CLOB 值用于插入
	// 某些驱动需要特殊包装
	WrapClobForInsert(value string) any

	// WrapBlobForInsert 包装 BLOB 值用于插入
	WrapBlobForInsert(value []byte) any

	// UnwrapQueryResult 解包查询结果
	// 将驱动特定的类型转换为标准 Go 类型
	UnwrapQueryResult(value any, typeName string) any

	// GetConnection 获取底层连接（用于高级操作）
	GetConnection(db *sql.DB) (any, error)

	// Ping 检查连接是否可用
	Ping(ctx context.Context, db *sql.DB) error
}

// Registry 驱动适配器注册表
var (
	registry   = map[DriverType]func() Adapter{}
	registryMu sync.RWMutex
)

// Register 注册驱动适配器
func Register(driverType DriverType, factory func() Adapter) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[driverType] = factory
}

// Get 获取驱动适配器
func Get(driverType DriverType) Adapter {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if factory, ok := registry[driverType]; ok {
		return factory()
	}
	return nil
}

// ListDrivers 列出所有已注册的驱动
func ListDrivers() []DriverType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]DriverType, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
