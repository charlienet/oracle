package tests

import (
	"context"
	"database/sql"
	"testing"

	oracle "github.com/charlienet/oracle"
	"github.com/sijms/go-ora/v2"
)

// TestProcedure_NoParams 测试无参数存储过程调用
func TestProcedure_NoParams(t *testing.T) {
	// 创建存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_no_params AS
		BEGIN
			NULL;
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_no_params")

	// 调用存储过程
	if err := DB.Exec("BEGIN test_no_params; END;").Error; err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}
}

// TestProcedure_InParams 测试 IN 参数存储过程
func TestProcedure_InParams(t *testing.T) {
	// 创建测试表
	DB.Exec("DROP TABLE test_proc_input")
	if err := DB.Exec(`
		CREATE TABLE test_proc_input (
			id NUMBER PRIMARY KEY,
			name VARCHAR2(100)
		)
	`).Error; err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}
	defer DB.Exec("DROP TABLE test_proc_input")

	// 创建带 IN 参数的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_in_params(
			p_id IN NUMBER,
			p_name IN VARCHAR2
		) AS
		BEGIN
			INSERT INTO test_proc_input (id, name) VALUES (p_id, p_name);
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_in_params")

	// 调用存储过程
	var id = 1
	var name = "test"
	if err := DB.Exec("BEGIN test_in_params(:1, :2); END;", id, name).Error; err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	// 验证数据
	var count int
	DB.Raw("SELECT COUNT(*) FROM test_proc_input WHERE id = ? AND name = ?", id, name).Scan(&count)
	if count != 1 {
		t.Errorf("数据未正确插入，期望 1 条，实际 %d 条", count)
	}
}

// TestProcedure_OutParams 测试 OUT 参数存储过程
func TestProcedure_OutParams(t *testing.T) {
	// 创建带 OUT 参数的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_out_params(
			p_result OUT VARCHAR2
		) AS
		BEGIN
			p_result := 'Hello from procedure';
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_out_params")

	// 调用存储过程并获取 OUT 参数
	var result string
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	// 使用本地包装结构，隐藏底层驱动细节
	outParam := oracle.OutParam(&result, 100)
	_, err = sqlDB.Exec("BEGIN test_out_params(:1); END;", oracle.ToDriverParam(outParam))
	if err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	if result != "Hello from procedure" {
		t.Errorf("OUT 参数值不正确，期望 'Hello from procedure'，实际 '%s'", result)
	}
}

// TestProcedure_InOutParams 测试 IN OUT 参数存储过程
func TestProcedure_InOutParams(t *testing.T) {
	// 创建带 IN OUT 参数的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_inout_params(
			p_value IN OUT NUMBER
		) AS
		BEGIN
			p_value := p_value * 2;
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_inout_params")

	// 调用存储过程
	var value = 5
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	// IN OUT 参数使用本地包装结构
	inOutParam := oracle.InOutParam(&value)
	_, err = sqlDB.Exec("BEGIN test_inout_params(:1); END;", oracle.ToDriverParam(inOutParam))
	if err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	if value != 10 {
		t.Errorf("IN OUT 参数值不正确，期望 10，实际 %d", value)
	}
}

// TestFunction_ReturnValue 测试函数调用
func TestFunction_ReturnValue(t *testing.T) {
	// 创建函数
	createFunc := `
		CREATE OR REPLACE FUNCTION test_function(
			p_a IN NUMBER,
			p_b IN NUMBER
		) RETURN NUMBER AS
		BEGIN
			RETURN p_a + p_b;
		END;
	`
	if err := DB.Exec(createFunc).Error; err != nil {
		t.Fatalf("创建函数失败: %v", err)
	}
	defer DB.Exec("DROP FUNCTION test_function")

	// 调用函数
	var result int
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	_, err = sqlDB.Exec("BEGIN :1 := test_function(:2, :3); END;", sql.Out{Dest: &result}, 3, 7)
	if err != nil {
		t.Errorf("调用函数失败: %v", err)
	}

	if result != 10 {
		t.Errorf("函数返回值不正确，期望 10，实际 %d", result)
	}
}

// TestProcedure_MultipleOutParams 测试多个 OUT 参数
func TestProcedure_MultipleOutParams(t *testing.T) {
	// 创建带多个 OUT 参数的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_multi_out(
			p_out1 OUT VARCHAR2,
			p_out2 OUT NUMBER,
			p_out3 OUT DATE
		) AS
		BEGIN
			p_out1 := 'test';
			p_out2 := 42;
			p_out3 := TO_DATE('2024-01-01', 'YYYY-MM-DD');
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_multi_out")

	// 调用存储过程
	var out1 string
	var out2 int
	var out3 string
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	// 多个 OUT 参数使用本地包装结构
	_, err = sqlDB.Exec("BEGIN test_multi_out(:1, :2, :3); END;",
		oracle.ToDriverParam(oracle.OutParam(&out1, 100)),
		oracle.ToDriverParam(oracle.OutParam(&out2, 0)),
		oracle.ToDriverParam(oracle.OutParam(&out3, 100)))
	if err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	if out1 != "test" {
		t.Errorf("OUT 参数 1 不正确，期望 'test'，实际 '%s'", out1)
	}
	if out2 != 42 {
		t.Errorf("OUT 参数 2 不正确，期望 42，实际 %d", out2)
	}
	if out3 == "" {
		t.Errorf("OUT 参数 3 为空")
	}
}

// TestProcedure_WithGoOraOut 测试使用 go-ora Out 参数
func TestProcedure_WithGoOraOut(t *testing.T) {
	// 创建存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_go_ora_out(
			p_result OUT VARCHAR2
		) AS
		BEGIN
			p_result := 'go-ora test';
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_go_ora_out")

	// 使用本地包装结构调用存储过程
	var result string
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	// VARCHAR2 OUT 参数使用本地包装结构
	_, err = sqlDB.Exec("BEGIN test_go_ora_out(:1); END;", oracle.ToDriverParam(oracle.OutParam(&result, 100)))
	if err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	if result != "go-ora test" {
		t.Errorf("OUT 参数值不正确，期望 'go-ora test'，实际 '%s'", result)
	}
}

// TestProcedure_Exception 测试存储过程异常处理
func TestProcedure_Exception(t *testing.T) {
	// 创建会抛出异常的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_exception AS
		BEGIN
			RAISE_APPLICATION_ERROR(-20001, 'Test exception');
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_exception")

	// 调用存储过程，期望捕获异常
	err := DB.Exec("BEGIN test_exception; END;").Error
	if err == nil {
		t.Errorf("期望捕获到异常，但未捕获到")
	}
}

// TestProcedure_CursorOutput 测试游标输出参数
func TestProcedure_CursorOutput(t *testing.T) {
	// 创建测试表
	DB.Exec("DROP TABLE test_cursor_data")
	if err := DB.Exec(`
		CREATE TABLE test_cursor_data (
			id NUMBER PRIMARY KEY,
			name VARCHAR2(100)
		)
	`).Error; err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}
	defer DB.Exec("DROP TABLE test_cursor_data")

	// 插入测试数据
	DB.Exec("INSERT INTO test_cursor_data VALUES (1, 'Alice')")
	DB.Exec("INSERT INTO test_cursor_data VALUES (2, 'Bob')")

	// 创建返回游标的存储过程
	createProc := `
		CREATE OR REPLACE PROCEDURE test_cursor_output(
			p_cursor OUT SYS_REFCURSOR
		) AS
		BEGIN
			OPEN p_cursor FOR SELECT id, name FROM test_cursor_data ORDER BY id;
		END;
	`
	if err := DB.Exec(createProc).Error; err != nil {
		t.Fatalf("创建存储过程失败: %v", err)
	}
	defer DB.Exec("DROP PROCEDURE test_cursor_output")

	// 调用存储过程并获取游标
	cursor := oracle.NewRefCursor()
	sqlDB, err := DB.DB()
	if err != nil {
		t.Fatalf("获取数据库连接失败: %v", err)
	}

	_, err = sqlDB.Exec("BEGIN test_cursor_output(:1); END;", oracle.ToDriverParam(oracle.OutParam(cursor.ToDriverCursor(), 0)))
	if err != nil {
		t.Errorf("调用存储过程失败: %v", err)
	}

	// 使用 WrapRefCursor 包装游标
	rows, err := go_ora.WrapRefCursor(context.Background(), sqlDB, cursor.ToDriverCursor())
	if err != nil {
		t.Errorf("包装游标失败: %v", err)
	}
	defer func() { _ = rows.Close() }()

	// 读取数据
	var count int
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("期望 2 条记录，实际 %d 条", count)
	}
}
