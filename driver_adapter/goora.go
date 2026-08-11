// Package driver_adapter 提供 Oracle 驱动抽象层
// 支持 go-ora 和 godror 两种底层驱动的切换
package driver_adapter

import (
	"context"
	"database/sql"
	go_ora "github.com/sijms/go-ora/v2"
)

// GoOraAdapter go-ora 驱动适配器
type GoOraAdapter struct{}

// goOraOutParam 包装 go_ora.Out
type goOraOutParam struct {
	out go_ora.Out
}

// GetDest 返回目标指针
func (o *goOraOutParam) GetDest() any {
	return o.out.Dest
}

// SetSize 设置缓冲区大小（用于字符串类型）
func (o *goOraOutParam) SetSize(size int) {
	o.out.Size = size
}

// GetSize 获取缓冲区大小
func (o *goOraOutParam) GetSize() int {
	return o.out.Size
}

// goOraLobData 包装 go_ora.Clob 和 go_ora.Blob
type goOraLobData struct {
	isClob  bool
	strVal  string
	byteVal []byte
	valid   bool
}

// IsCLOB 是否为 CLOB 类型
func (l *goOraLobData) IsCLOB() bool {
	return l.isClob
}

// IsBLOB 是否为 BLOB 类型
func (l *goOraLobData) IsBLOB() bool {
	return !l.isClob
}

// GetString 获取字符串值（CLOB）
func (l *goOraLobData) GetString() string {
	return l.strVal
}

// GetBytes 获取字节值（BLOB）
func (l *goOraLobData) GetBytes() []byte {
	return l.byteVal
}

// IsValid 是否有效
func (l *goOraLobData) IsValid() bool {
	return l.valid
}

// goOraBatchData 包装批量数据
type goOraBatchData struct {
	values []any
}

// Len 返回数据长度
func (b *goOraBatchData) Len() int {
	return len(b.values)
}

// GetValues 返回所有值
func (b *goOraBatchData) GetValues() []any {
	return b.values
}

// Name 返回驱动名称
func (a *GoOraAdapter) Name() string {
	return "go-ora"
}

// Type 返回驱动类型
func (a *GoOraAdapter) Type() DriverType {
	return DriverGoOra
}

// Open 打开数据库连接
func (a *GoOraAdapter) Open(dsn string) (*sql.DB, error) {
	return sql.Open("oracle", dsn)
}

// CreateOutParam 创建输出参数（用于 RETURNING INTO）
func (a *GoOraAdapter) CreateOutParam(dest any, size int) OutParam {
	return &goOraOutParam{
		out: go_ora.Out{Dest: dest, Size: size},
	}
}

// CreateClob 创建 CLOB 数据
func (a *GoOraAdapter) CreateClob(value string) LobData {
	return &goOraLobData{
		isClob:  true,
		strVal:  value,
		byteVal: nil,
		valid:   true,
	}
}

// CreateBlob 创建 BLOB 数据
func (a *GoOraAdapter) CreateBlob(value []byte) LobData {
	return &goOraLobData{
		isClob:  false,
		strVal:  "",
		byteVal: value,
		valid:   true,
	}
}

// CreateBatch 创建批量数据
func (a *GoOraAdapter) CreateBatch(values []any) BatchData {
	return &goOraBatchData{
		values: values,
	}
}

// NeedsSizeForOut 返回输出参数是否需要指定 Size
func (a *GoOraAdapter) NeedsSizeForOut() bool {
	return true
}

// SupportsReturningMultiRow 返回是否支持多行 RETURNING
func (a *GoOraAdapter) SupportsReturningMultiRow() bool {
	return false
}

// SupportsBulkCopy 返回是否支持 BulkCopy
func (a *GoOraAdapter) SupportsBulkCopy() bool {
	return true
}

// WrapClobForInsert 包装 CLOB 值用于插入
func (a *GoOraAdapter) WrapClobForInsert(value string) any {
	return go_ora.Clob{String: value, Valid: true}
}

// WrapBlobForInsert 包装 BLOB 值用于插入
func (a *GoOraAdapter) WrapBlobForInsert(value []byte) any {
	return go_ora.Blob{Data: value, Valid: true}
}

// UnwrapQueryResult 解包查询结果
func (a *GoOraAdapter) UnwrapQueryResult(value any, typeName string) any {
	// 根据需要处理 go-ora 特有的返回类型转换
	// 这里简单返回原始值，可根据实际需求扩展
	return value
}

// GetConnection 获取底层连接（用于高级操作）
func (a *GoOraAdapter) GetConnection(db *sql.DB) (any, error) {
	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var rawConn any
	err = conn.Raw(func(driverConn any) error {
		rawConn = driverConn
		return nil
	})

	return rawConn, err
}

// Ping 检查连接是否可用
func (a *GoOraAdapter) Ping(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}

// init 函数中注册驱动
func init() {
	Register(DriverGoOra, func() Adapter {
		return &GoOraAdapter{}
	})
}
