package oracle

import (
	"context"
	"database/sql/driver"
	"reflect"

	go_ora "github.com/sijms/go-ora/v2"
)

// enumNormalizeDriver 包装 go-ora 驱动。
//
// go-ora 的 CheckNamedValue 对非 driver.Valuer 一律返回 nil，切断了
// database/sql 的 DefaultParameterConverter 通道，导致命名基本类型（Go 枚举）
// 在 go-ora 的 setDataType 中因类型相等比较失败而报 "unsupported go type"。
//
// 解决方案：在 CheckNamedValue 中对命名基本类型返回 driver.ErrSkip，
// 让 database/sql 通过 defaultCheckNamedValue → DefaultParameterConverter.ConvertValue
// 自动将枚举值转换为裸 driver.Value，无需手工 reflect 规范化。
type enumNormalizeDriver struct {
	inner *go_ora.OracleDriver
}

// Open 打开连接并包装为 enumNormalizeConn。
func (d *enumNormalizeDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &enumNormalizeConn{inner: conn}, nil
}

// OpenConnector 创建连接器并包装为 enumNormalizeConnector。
func (d *enumNormalizeDriver) OpenConnector(connString string) (driver.Connector, error) {
	connector, err := d.inner.OpenConnector(connString)
	if err != nil {
		return nil, err
	}
	return &enumNormalizeConnector{inner: connector, drv: d}, nil
}

// enumNormalizeConnector 包装 driver.Connector。
type enumNormalizeConnector struct {
	inner driver.Connector
	drv   *enumNormalizeDriver
}

// Connect 创建连接并包装为 enumNormalizeConn。
func (c *enumNormalizeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &enumNormalizeConn{inner: conn}, nil
}

// Driver 返回包装后的驱动，保证 database/sql 拿到的 Driver 与连接一致。
func (c *enumNormalizeConnector) Driver() driver.Driver {
	return c.drv
}

// enumNormalizeConn 包装 driver.Conn，在 CheckNamedValue 中委托
// database/sql 默认转换处理命名基本类型（Go 枚举）。
type enumNormalizeConn struct {
	inner driver.Conn
}

// Prepare 转发到内部连接的 Prepare，并将返回的 Stmt 包装为 enumNormalizeStmt，
// 以覆盖 prepared 路径的枚举规范化。
func (c *enumNormalizeConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &enumNormalizeStmt{inner: stmt}, nil
}

// Close 转发到内部连接的 Close。
func (c *enumNormalizeConn) Close() error {
	return c.inner.Close()
}

// Begin 转发到内部连接的 Begin。
func (c *enumNormalizeConn) Begin() (driver.Tx, error) {
	return c.inner.Begin() //nolint:staticcheck // driver.Conn 接口契约必需方法，转发内部实现
}

// PrepareContext 转发到 driver.ConnPrepareContext。
func (c *enumNormalizeConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.inner.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &enumNormalizeStmt{inner: stmt}, nil
}

// ExecContext 转发到 driver.ExecerContext。
func (c *enumNormalizeConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.inner.(driver.ExecerContext).ExecContext(ctx, query, args)
}

// QueryContext 转发到 driver.QueryerContext。
func (c *enumNormalizeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
}

// BeginTx 转发到 driver.ConnBeginTx。
func (c *enumNormalizeConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.inner.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

// Ping 转发到 driver.Pinger。
func (c *enumNormalizeConn) Ping(ctx context.Context) error {
	return c.inner.(driver.Pinger).Ping(ctx)
}

// ResetSession 转发到 driver.SessionResetter。
func (c *enumNormalizeConn) ResetSession(ctx context.Context) error {
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

// CheckNamedValue 在直接执行路径（不经 Prepare 的 Exec/Query）上处理枚举参数：
// 对命名基本类型（Go 枚举）返回 driver.ErrSkip，委托 database/sql 默认转换
// （defaultCheckNamedValue → DefaultParameterConverter.ConvertValue）完成规范化。
// 其余值转发给 go-ora 内部的 NamedValueChecker。
func (c *enumNormalizeConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checkEnumNamedValue(nv) {
		return driver.ErrSkip
	}
	if nc, ok := c.inner.(driver.NamedValueChecker); ok {
		return nc.CheckNamedValue(nv)
	}
	return nil
}

// enumNormalizeStmt 包装 driver.Stmt，在 prepared 路径上同样委托
// database/sql 默认转换处理枚举参数。
//
// database/sql 对 prepared 语句优先使用 Stmt 的 NamedValueChecker
// （convert.go: nvc, ok := si.(driver.NamedValueChecker); if !ok { nvc, _ = ci.(...) }），
// go-ora 的 *go_ora.Stmt 实现了 CheckNamedValue（command.go:1864）。若只包装
// 连接层，gorm.Config{PrepareStmt: true} 时连接级规范化会被 Stmt 级 checker
// 绕过，因此必须包装 Stmt。
type enumNormalizeStmt struct {
	inner driver.Stmt
}

// Close 转发到内部 Stmt 的 Close。
func (s *enumNormalizeStmt) Close() error {
	return s.inner.Close()
}

// NumInput 转发到内部 Stmt 的 NumInput。
func (s *enumNormalizeStmt) NumInput() int {
	return s.inner.NumInput()
}

// Exec 转发到内部 Stmt 的 Exec。
func (s *enumNormalizeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.inner.Exec(args) //nolint:staticcheck // driver.Stmt 接口契约必需方法，转发内部实现
}

// Query 转发到内部 Stmt 的 Query。
func (s *enumNormalizeStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.inner.Query(args) //nolint:staticcheck // driver.Stmt 接口契约必需方法，转发内部实现
}

// ExecContext 转发到 driver.StmtExecContext。
func (s *enumNormalizeStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.inner.(driver.StmtExecContext).ExecContext(ctx, args)
}

// QueryContext 转发到 driver.StmtQueryContext。
func (s *enumNormalizeStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.inner.(driver.StmtQueryContext).QueryContext(ctx, args)
}

// CheckNamedValue 在 prepared 路径上处理枚举参数：对命名基本类型返回
// driver.ErrSkip，委托 database/sql 默认转换；其余值转发给 go-ora 内部
// Stmt 的 NamedValueChecker。
func (s *enumNormalizeStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if checkEnumNamedValue(nv) {
		return driver.ErrSkip
	}
	if nc, ok := s.inner.(driver.NamedValueChecker); ok {
		return nc.CheckNamedValue(nv)
	}
	return nil
}

// checkEnumNamedValue 判断是否应将参数交给 database/sql 默认转换：
// 返回 true 时调用方应返回 driver.ErrSkip，database/sql 随后会调用
// defaultCheckNamedValue → DefaultParameterConverter.ConvertValue 完成
// 命名基本类型（Go 枚举）到裸 driver.Value 的转换。
func checkEnumNamedValue(nv *driver.NamedValue) bool {
	if nv.Value == nil {
		return false
	}
	// 已实现 driver.Valuer 的值：ErrSkip 后由默认转换调用 Value()
	if _, ok := nv.Value.(driver.Valuer); ok {
		return true
	}
	// 已是裸 driver.Value（int64/float64/bool/[]byte/string/time.Time）：接受
	if driver.IsValue(nv.Value) {
		return false
	}
	// 命名基本类型（解指针后 Kind 为数值/布尔/字符串）→ 委托默认转换；
	// 其余（struct/Out/UDT 等）保持原样，交给 go-ora 自己的 checker
	rv := reflect.ValueOf(nv.Value)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.Bool, reflect.String:
		return true
	}
	return false
}
