//go:build godror

package driver_adapter

import (
	"context"
	"database/sql"
	"fmt"
)

// GodrorAdapter godror 驱动适配器
type GodrorAdapter struct{}

// godrorOutParam godror 输出参数包装
type godrorOutParam struct {
	dest interface{}
	size int
}

func (p *godrorOutParam) GetDest() interface{} {
	return p.dest
}

func (p *godrorOutParam) SetSize(size int) {
	p.size = size
}

func (p *godrorOutParam) GetSize() int {
	return p.size
}

// godrorLobData godror LOB 数据包装
type godrorLobData struct {
	isClob  bool
	strVal  string
	byteVal []byte
	valid   bool
}

func (l *godrorLobData) IsCLOB() bool {
	return l.isClob
}

func (l *godrorLobData) IsBLOB() bool {
	return !l.isClob
}

func (l *godrorLobData) GetString() string {
	return l.strVal
}

func (l *godrorLobData) GetBytes() []byte {
	return l.byteVal
}

func (l *godrorLobData) IsValid() bool {
	return l.valid
}

// godrorBatchData godror 批量数据包装
type godrorBatchData struct {
	values []interface{}
}

func (b *godrorBatchData) Len() int {
	return len(b.values)
}

func (b *godrorBatchData) GetValues() []interface{} {
	return b.values
}

// Name 返回驱动名称
func (a *GodrorAdapter) Name() string {
	return "godror"
}

// Type 返回驱动类型
func (a *GodrorAdapter) Type() DriverType {
	return DriverGodror
}

// Open 打开数据库连接
func (a *GodrorAdapter) Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("godror", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open connection with godror: %w", err)
	}
	return db, nil
}

// CreateOutParam 创建输出参数（用于 RETURNING INTO）
func (a *GodrorAdapter) CreateOutParam(dest interface{}, size int) OutParam {
	return &godrorOutParam{
		dest: dest,
		size: size,
	}
}

// CreateClob 创建 CLOB 数据
func (a *GodrorAdapter) CreateClob(value string) LobData {
	return &godrorLobData{
		isClob:  true,
		strVal:  value,
		valid:   true,
		byteVal: nil,
	}
}

// CreateBlob 创建 BLOB 数据
func (a *GodrorAdapter) CreateBlob(value []byte) LobData {
	return &godrorLobData{
		isClob:  false,
		byteVal: value,
		valid:   true,
		strVal:  "",
	}
}

// CreateBatch 创建批量数据
func (a *GodrorAdapter) CreateBatch(values []interface{}) BatchData {
	return &godrorBatchData{
		values: values,
	}
}

// NeedsSizeForOut 返回输出参数是否需要指定 Size
func (a *GodrorAdapter) NeedsSizeForOut() bool {
	return false
}

// SupportsReturningMultiRow 返回是否支持多行 RETURNING
func (a *GodrorAdapter) SupportsReturningMultiRow() bool {
	return true
}

// SupportsBulkCopy 返回是否支持 BulkCopy
func (a *GodrorAdapter) SupportsBulkCopy() bool {
	return false
}

// WrapClobForInsert 包装 CLOB 值用于插入
func (a *GodrorAdapter) WrapClobForInsert(value string) interface{} {
	// godror 可以直接处理字符串作为 CLOB
	return value
}

// WrapBlobForInsert 包装 BLOB 值用于插入
func (a *GodrorAdapter) WrapBlobForInsert(value []byte) interface{} {
	// godror 可以直接处理字节数组作为 BLOB
	return value
}

// UnwrapQueryResult 解包查询结果
func (a *GodrorAdapter) UnwrapQueryResult(value interface{}, typeName string) interface{} {
	// godror 通常返回标准 Go 类型，无需特殊处理
	// 但如果遇到特定类型，可以在这里进行转换
	switch v := value.(type) {
	case *string:
		if v == nil {
			return nil
		}
		return *v
	case *[]byte:
		if v == nil {
			return nil
		}
		return *v
	default:
		return value
	}
}

// GetConnection 获取底层连接（用于高级操作）
func (a *GodrorAdapter) GetConnection(db *sql.DB) (interface{}, error) {
	// 从 sql.DB 获取原始连接
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// 返回原始连接
	return conn, nil
}

// Ping 检查连接是否可用
func (a *GodrorAdapter) Ping(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}

// init 注册 godror 驱动适配器
func init() {
	Register(DriverGodror, func() Adapter {
		return &GodrorAdapter{}
	})
}