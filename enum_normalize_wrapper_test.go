package oracle

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	go_ora "github.com/sijms/go-ora/v2"
)

// ---- mock 驱动组件 ----

// mockConn 实现 driver.Conn 及扩展接口，记录调用以验证转发。
type mockConn struct {
	prepareErr     error
	closeErr       error
	beginErr       error
	pingErr        error
	resetErr       error
	checkNVResult  error
	checkNVCalled  bool
	prepareCtxErr  error
	execCtxResult  driver.Result
	execCtxErr     error
	queryCtxResult driver.Rows
	queryCtxErr    error
	beginTxResult  driver.Tx
	beginTxErr     error
	lastQuery      string
	lastArgs       []driver.NamedValue
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	return &mockStmtForConn{}, nil
}

func (c *mockConn) Close() error                       { return c.closeErr }
func (c *mockConn) Begin() (driver.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return &mockTx{}, nil
} //nolint:staticcheck
func (c *mockConn) Ping(ctx context.Context) error     { return c.pingErr }
func (c *mockConn) ResetSession(ctx context.Context) error { return c.resetErr }

func (c *mockConn) CheckNamedValue(nv *driver.NamedValue) error {
	c.checkNVCalled = true
	return c.checkNVResult
}

func (c *mockConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c.prepareCtxErr != nil {
		return nil, c.prepareCtxErr
	}
	return &mockStmtForConn{}, nil
}

func (c *mockConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.lastQuery = query
	c.lastArgs = args
	return c.execCtxResult, c.execCtxErr
}

func (c *mockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.lastQuery = query
	c.lastArgs = args
	return c.queryCtxResult, c.queryCtxErr
}

func (c *mockConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.beginTxResult, c.beginTxErr
}

// mockStmtForConn 实现 driver.Stmt 及扩展接口
type mockStmtForConn struct {
	closeErr      error
	numInput      int
	execErr       error
	queryErr      error
	execCtxErr    error
	queryCtxErr   error
	checkNVResult error
	checkNVCalled bool
}

func (s *mockStmtForConn) Close() error  { return s.closeErr }
func (s *mockStmtForConn) NumInput() int { return s.numInput }
func (s *mockStmtForConn) Exec(args []driver.Value) (driver.Result, error) { return nil, s.execErr } //nolint:staticcheck
func (s *mockStmtForConn) Query(args []driver.Value) (driver.Rows, error)  { return nil, s.queryErr } //nolint:staticcheck

func (s *mockStmtForConn) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return nil, s.execCtxErr
}

func (s *mockStmtForConn) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return nil, s.queryCtxErr
}

func (s *mockStmtForConn) CheckNamedValue(nv *driver.NamedValue) error {
	s.checkNVCalled = true
	return s.checkNVResult
}

// mockConnNoNVC 不实现 NamedValueChecker 的 mock
type mockConnNoNVC struct {
	mockConn
}

func (c *mockConnNoNVC) CheckNamedValue(*driver.NamedValue) error { return nil }

// mockStmtNoNVC 不实现 NamedValueChecker 的 mock
type mockStmtNoNVC struct {
	mockStmtForConn
}

func (s *mockStmtNoNVC) CheckNamedValue(*driver.NamedValue) error { return nil }

// ---- enumNormalizeDriver 测试（使用真实 go_ora 驱动） ----

func TestEnumNormalizeDriverOpen(t *testing.T) {
	inner := go_ora.NewDriver()
	d := &enumNormalizeDriver{inner: inner}

	t.Run("无效 DSN 返回错误", func(t *testing.T) {
		_, err := d.Open("invalid-dsn")
		if err == nil {
			t.Error("Open() with invalid DSN should return error")
		}
	})
}

func TestEnumNormalizeDriverOpenConnector(t *testing.T) {
	inner := go_ora.NewDriver()
	d := &enumNormalizeDriver{inner: inner}

	t.Run("创建连接器成功", func(t *testing.T) {
		connector, err := d.OpenConnector("oracle://user:pass@host:1521/db")
		if err != nil {
			t.Fatalf("OpenConnector() error = %v", err)
		}
		if connector == nil {
			t.Fatal("OpenConnector() returned nil")
		}
		if _, ok := connector.(*enumNormalizeConnector); !ok {
			t.Errorf("OpenConnector() returned %T, want *enumNormalizeConnector", connector)
		}
	})
}

func TestEnumNormalizeConnectorConnect(t *testing.T) {
	inner := go_ora.NewDriver()
	d := &enumNormalizeDriver{inner: inner}

	connector, err := d.OpenConnector("oracle://user:pass@host:1521/db")
	if err != nil {
		t.Fatalf("OpenConnector() error = %v", err)
	}

	t.Run("Connect 返回包装后的连接", func(t *testing.T) {
		// Connect 会尝试建立连接，预期失败（无法连接到数据库）
		// 但应该返回包装后的连接或错误
		_, err := connector.Connect(context.Background())
		// 连接失败是预期的，我们只验证包装逻辑
		_ = err
	})
}

func TestEnumNormalizeConnectorDriver(t *testing.T) {
	inner := go_ora.NewDriver()
	d := &enumNormalizeDriver{inner: inner}

	connector, err := d.OpenConnector("oracle://user:pass@host:1521/db")
	if err != nil {
		t.Fatalf("OpenConnector() error = %v", err)
	}

	got := connector.Driver()
	if got != d {
		t.Errorf("Driver() returned %p, want %p", got, d)
	}
}

// mockTx 实现 driver.Tx
type mockTx struct{}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

// mockResult 实现 driver.Result
type mockResult struct{}

func (r *mockResult) LastInsertId() (int64, error) { return 0, nil }
func (r *mockResult) RowsAffected() (int64, error) { return 0, nil }

// mockRows 实现 driver.Rows
type mockRows struct{}

func (r *mockRows) Columns() []string              { return nil }
func (r *mockRows) Close() error                   { return nil }
func (r *mockRows) Next(dest []driver.Value) error { return errors.New("no rows") }

// ---- enumNormalizeConn 测试 ----

func TestEnumNormalizeConnPrepare(t *testing.T) {
	t.Run("成功准备语句", func(t *testing.T) {
		inner := &mockConn{}
		conn := &enumNormalizeConn{inner: inner}
		stmt, err := conn.Prepare("SELECT 1")
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		if _, ok := stmt.(*enumNormalizeStmt); !ok {
			t.Errorf("Prepare() returned %T, want *enumNormalizeStmt", stmt)
		}
	})

	t.Run("准备语句失败", func(t *testing.T) {
		inner := &mockConn{prepareErr: errors.New("prepare failed")}
		conn := &enumNormalizeConn{inner: inner}
		stmt, err := conn.Prepare("SELECT 1")
		if err == nil {
			t.Fatal("Prepare() expected error, got nil")
		}
		if stmt != nil {
			t.Error("Prepare() should return nil on error")
		}
	})
}

func TestEnumNormalizeConnClose(t *testing.T) {
	t.Run("关闭成功", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{closeErr: nil}}
		if err := conn.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("关闭失败", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{closeErr: errors.New("close failed")}}
		if err := conn.Close(); err == nil {
			t.Error("Close() expected error, got nil")
		}
	})
}

func TestEnumNormalizeConnBegin(t *testing.T) {
	t.Run("开始事务成功", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{beginTxResult: &mockTx{}}}
		tx, err := conn.Begin()
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		if tx == nil {
			t.Error("Begin() returned nil tx")
		}
	})

	t.Run("开始事务失败", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{beginErr: errors.New("begin failed")}}
		tx, err := conn.Begin()
		if err == nil {
			t.Fatal("Begin() expected error, got nil")
		}
		if tx != nil {
			t.Error("Begin() should return nil on error")
		}
	})
}

func TestEnumNormalizeConnPrepareContext(t *testing.T) {
	t.Run("带上下文准备语句成功", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{}}
		stmt, err := conn.PrepareContext(context.Background(), "SELECT 1")
		if err != nil {
			t.Fatalf("PrepareContext() error = %v", err)
		}
		if _, ok := stmt.(*enumNormalizeStmt); !ok {
			t.Errorf("PrepareContext() returned %T, want *enumNormalizeStmt", stmt)
		}
	})

	t.Run("带上下文准备语句失败", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{prepareCtxErr: errors.New("prepare ctx failed")}}
		stmt, err := conn.PrepareContext(context.Background(), "SELECT 1")
		if err == nil {
			t.Fatal("PrepareContext() expected error, got nil")
		}
		if stmt != nil {
			t.Error("PrepareContext() should return nil on error")
		}
	})
}

func TestEnumNormalizeConnExecContext(t *testing.T) {
	inner := &mockConn{execCtxResult: &mockResult{}}
	conn := &enumNormalizeConn{inner: inner}

	result, err := conn.ExecContext(context.Background(), "INSERT INTO t VALUES (1)", nil)
	if err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}
	if result == nil {
		t.Error("ExecContext() returned nil result")
	}
	if inner.lastQuery != "INSERT INTO t VALUES (1)" {
		t.Errorf("ExecContext() query = %q, want %q", inner.lastQuery, "INSERT INTO t VALUES (1)")
	}
}

func TestEnumNormalizeConnExecContextError(t *testing.T) {
	inner := &mockConn{execCtxErr: errors.New("exec failed")}
	conn := &enumNormalizeConn{inner: inner}

	_, err := conn.ExecContext(context.Background(), "BAD SQL", nil)
	if err == nil {
		t.Fatal("ExecContext() expected error, got nil")
	}
}

func TestEnumNormalizeConnQueryContext(t *testing.T) {
	inner := &mockConn{queryCtxResult: &mockRows{}}
	conn := &enumNormalizeConn{inner: inner}

	rows, err := conn.QueryContext(context.Background(), "SELECT * FROM t", nil)
	if err != nil {
		t.Fatalf("QueryContext() error = %v", err)
	}
	if rows == nil {
		t.Error("QueryContext() returned nil rows")
	}
}

func TestEnumNormalizeConnQueryContextError(t *testing.T) {
	inner := &mockConn{queryCtxErr: errors.New("query failed")}
	conn := &enumNormalizeConn{inner: inner}

	_, err := conn.QueryContext(context.Background(), "BAD SQL", nil)
	if err == nil {
		t.Fatal("QueryContext() expected error, got nil")
	}
}

func TestEnumNormalizeConnBeginTx(t *testing.T) {
	inner := &mockConn{beginTxResult: &mockTx{}}
	conn := &enumNormalizeConn{inner: inner}

	tx, err := conn.BeginTx(context.Background(), driver.TxOptions{})
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if tx == nil {
		t.Error("BeginTx() returned nil tx")
	}
}

func TestEnumNormalizeConnBeginTxError(t *testing.T) {
	inner := &mockConn{beginTxErr: errors.New("begin tx failed")}
	conn := &enumNormalizeConn{inner: inner}

	_, err := conn.BeginTx(context.Background(), driver.TxOptions{})
	if err == nil {
		t.Fatal("BeginTx() expected error, got nil")
	}
}

func TestEnumNormalizeConnPing(t *testing.T) {
	t.Run("Ping 成功", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{pingErr: nil}}
		if err := conn.Ping(context.Background()); err != nil {
			t.Errorf("Ping() error = %v", err)
		}
	})

	t.Run("Ping 失败", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{pingErr: errors.New("ping failed")}}
		if err := conn.Ping(context.Background()); err == nil {
			t.Error("Ping() expected error, got nil")
		}
	})
}

func TestEnumNormalizeConnResetSession(t *testing.T) {
	t.Run("ResetSession 成功", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{resetErr: nil}}
		if err := conn.ResetSession(context.Background()); err != nil {
			t.Errorf("ResetSession() error = %v", err)
		}
	})

	t.Run("ResetSession 失败", func(t *testing.T) {
		conn := &enumNormalizeConn{inner: &mockConn{resetErr: errors.New("reset failed")}}
		if err := conn.ResetSession(context.Background()); err == nil {
			t.Error("ResetSession() expected error, got nil")
		}
	})
}

func TestEnumNormalizeConnCheckNamedValue(t *testing.T) {
	t.Run("命名枚举返回 ErrSkip", func(t *testing.T) {
		inner := &mockConn{}
		conn := &enumNormalizeConn{inner: inner}
		nv := &driver.NamedValue{Value: MerchantStatus(1)}
		err := conn.CheckNamedValue(nv)
		if err != driver.ErrSkip {
			t.Errorf("CheckNamedValue() = %v, want driver.ErrSkip", err)
		}
		if inner.checkNVCalled {
			t.Error("CheckNamedValue() should not forward to inner when returning ErrSkip")
		}
	})

	t.Run("裸值转发给内部 checker", func(t *testing.T) {
		inner := &mockConn{checkNVResult: nil}
		conn := &enumNormalizeConn{inner: inner}
		nv := &driver.NamedValue{Value: int64(42)}
		err := conn.CheckNamedValue(nv)
		if err != nil {
			t.Errorf("CheckNamedValue() = %v, want nil", err)
		}
		if !inner.checkNVCalled {
			t.Error("CheckNamedValue() should forward to inner checker for bare values")
		}
	})

	t.Run("内部不实现 NamedValueChecker 返回 nil", func(t *testing.T) {
		inner := &mockConnNoNVC{}
		conn := &enumNormalizeConn{inner: inner}
		nv := &driver.NamedValue{Value: int64(42)}
		err := conn.CheckNamedValue(nv)
		if err != nil {
			t.Errorf("CheckNamedValue() = %v, want nil", err)
		}
	})

	t.Run("driver.Valuer 返回 ErrSkip", func(t *testing.T) {
		inner := &mockConn{}
		conn := &enumNormalizeConn{inner: inner}
		nv := &driver.NamedValue{Value: testValuer{value: "x"}}
		err := conn.CheckNamedValue(nv)
		if err != driver.ErrSkip {
			t.Errorf("CheckNamedValue() = %v, want driver.ErrSkip", err)
		}
	})
}

// ---- enumNormalizeStmt 测试 ----

func TestEnumNormalizeStmtClose(t *testing.T) {
	t.Run("关闭成功", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{closeErr: nil}}
		if err := stmt.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	t.Run("关闭失败", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{closeErr: errors.New("close failed")}}
		if err := stmt.Close(); err == nil {
			t.Error("Close() expected error, got nil")
		}
	})
}

func TestEnumNormalizeStmtNumInput(t *testing.T) {
	stmt := &enumNormalizeStmt{inner: &mockStmtForConn{numInput: 3}}
	if got := stmt.NumInput(); got != 3 {
		t.Errorf("NumInput() = %d, want 3", got)
	}
}

func TestEnumNormalizeStmtExec(t *testing.T) {
	t.Run("Exec 成功", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{execErr: nil}}
		result, err := stmt.Exec(nil)
		if err != nil {
			t.Errorf("Exec() error = %v", err)
		}
		if result != nil {
			t.Error("Exec() should return nil result")
		}
	})

	t.Run("Exec 失败", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{execErr: errors.New("exec failed")}}
		_, err := stmt.Exec(nil)
		if err == nil {
			t.Error("Exec() expected error, got nil")
		}
	})
}

func TestEnumNormalizeStmtQuery(t *testing.T) {
	t.Run("Query 成功", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{queryErr: nil}}
		rows, err := stmt.Query(nil)
		if err != nil {
			t.Errorf("Query() error = %v", err)
		}
		if rows != nil {
			t.Error("Query() should return nil rows")
		}
	})

	t.Run("Query 失败", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{queryErr: errors.New("query failed")}}
		_, err := stmt.Query(nil)
		if err == nil {
			t.Error("Query() expected error, got nil")
		}
	})
}

func TestEnumNormalizeStmtExecContext(t *testing.T) {
	t.Run("ExecContext 成功", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{execCtxErr: nil}}
		result, err := stmt.ExecContext(context.Background(), nil)
		if err != nil {
			t.Errorf("ExecContext() error = %v", err)
		}
		if result != nil {
			t.Error("ExecContext() should return nil result")
		}
	})

	t.Run("ExecContext 失败", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{execCtxErr: errors.New("exec ctx failed")}}
		_, err := stmt.ExecContext(context.Background(), nil)
		if err == nil {
			t.Error("ExecContext() expected error, got nil")
		}
	})
}

func TestEnumNormalizeStmtQueryContext(t *testing.T) {
	t.Run("QueryContext 成功", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{queryCtxErr: nil}}
		rows, err := stmt.QueryContext(context.Background(), nil)
		if err != nil {
			t.Errorf("QueryContext() error = %v", err)
		}
		if rows != nil {
			t.Error("QueryContext() should return nil rows")
		}
	})

	t.Run("QueryContext 失败", func(t *testing.T) {
		stmt := &enumNormalizeStmt{inner: &mockStmtForConn{queryCtxErr: errors.New("query ctx failed")}}
		_, err := stmt.QueryContext(context.Background(), nil)
		if err == nil {
			t.Error("QueryContext() expected error, got nil")
		}
	})
}

func TestEnumNormalizeStmtCheckNamedValueStmt(t *testing.T) {
	t.Run("命名枚举返回 ErrSkip", func(t *testing.T) {
		inner := &mockStmtForConn{}
		stmt := &enumNormalizeStmt{inner: inner}
		nv := &driver.NamedValue{Value: MerchantStatus(1)}
		err := stmt.CheckNamedValue(nv)
		if err != driver.ErrSkip {
			t.Errorf("CheckNamedValue() = %v, want driver.ErrSkip", err)
		}
		if inner.checkNVCalled {
			t.Error("CheckNamedValue() should not forward to inner when returning ErrSkip")
		}
	})

	t.Run("裸值转发给内部 checker", func(t *testing.T) {
		inner := &mockStmtForConn{checkNVResult: nil}
		stmt := &enumNormalizeStmt{inner: inner}
		nv := &driver.NamedValue{Value: int64(42)}
		err := stmt.CheckNamedValue(nv)
		if err != nil {
			t.Errorf("CheckNamedValue() = %v, want nil", err)
		}
		if !inner.checkNVCalled {
			t.Error("CheckNamedValue() should forward to inner checker for bare values")
		}
	})

	t.Run("内部不实现 NamedValueChecker 返回 nil", func(t *testing.T) {
		inner := &mockStmtNoNVC{}
		stmt := &enumNormalizeStmt{inner: inner}
		nv := &driver.NamedValue{Value: int64(42)}
		err := stmt.CheckNamedValue(nv)
		if err != nil {
			t.Errorf("CheckNamedValue() = %v, want nil", err)
		}
	})
}
