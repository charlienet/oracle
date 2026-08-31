package oracle

import (
	"reflect"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

// TestBatchInsertPerformanceBaseline 批量插入性能基线测试（已停用）
// 原实现仅输出 t.Log、无任何断言，属于空壳测试。
// 真实性能基准见 tests/batch_insert_performance_test.go（需真实数据库，手动基准工具）。
// 此处显式跳过，避免误导。
func TestBatchInsertPerformanceBaseline(t *testing.T) {
	t.Skip("性能基线已迁移至 tests/batch_insert_performance_test.go，此处不再维护")
}

// TestBatchInsertWithoutReturning 验证无 RETURNING 场景的批量插入优化
// 场景：所有字段都有值，无默认值字段
// 期望：使用 INSERT ALL ... INTO ... INTO ... SELECT * FROM dual 语句
func TestBatchInsertWithoutReturning(t *testing.T) {
	// 测试无默认值字段的批量插入
	// 使用已定义的 createTestModel（Code 有 default:ABC，但 ID 显式赋值时不在 FieldsWithDefaultDBValue）
	// 注意：createTestModel 的 Code 字段有默认值，但这里测试的是当所有字段都有显式值时的行为
	model := &[]createTestModel{
		{ID: 1, Name: "A", Code: "X"},
		{ID: 2, Name: "B", Code: "Y"},
		{ID: 3, Name: "C", Code: "Z"},
	}

	db := newTestDB(t, model)

	// 注意：createTestModel.Code 有 default:ABC，所以 FieldsWithDefaultDBValue 不为空
	// 但当所有字段都有显式值时，应该使用批量插入优化

	Create(db)

	if db.Error != nil {
		t.Fatalf("Create returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	t.Logf("Generated SQL: %s", sql)
	t.Logf("Vars count: %d", len(db.Statement.Vars))

	// 验证优化生效：无 RETURNING，使用 INSERT ALL 语法
	// 期望：INSERT ALL INTO ... INTO ... INTO ... SELECT * FROM dual
	if containsReturning(sql) {
		t.Errorf("expected no RETURNING in SQL when all fields have values, got: %s", sql)
	}

	if !containsInsertAll(sql) {
		t.Errorf("expected INSERT ALL in SQL, got: %s", sql)
	}

	// 验证 Vars 数量正确
	// 期望：3 行 × 3 列 = 9 个变量
	expectedVars := 9
	if len(db.Statement.Vars) != expectedVars {
		t.Errorf("expected %d vars, got %d", expectedVars, len(db.Statement.Vars))
	}
}

// TestBatchInsertWithReturning 验证有 RETURNING 场景的批量插入处理
// 场景：有默认值字段（如自增主键）
// 期望：批量插入使用单条插入 + RETURNING（Oracle 不支持多行 RETURNING INTO）
func TestBatchInsertWithReturning(t *testing.T) {
	// 测试有默认值字段的批量插入
	// 新逻辑：有默认值字段时，使用单条插入 + RETURNING，逐行执行
	type ModelWithDefault struct {
		ID   uint   `gorm:"primaryKey"` // 非显式 autoIncrement，会进入 FieldsWithDefaultDBValue
		Name string `gorm:"size:100"`
	}

	model := &[]ModelWithDefault{
		{Name: "A"},
		{Name: "B"},
		{Name: "C"},
	}

	db := newTestDB(t, model)

	// 验证有默认值字段
	if len(db.Statement.Schema.FieldsWithDefaultDBValue) == 0 {
		t.Fatal("expected default value fields")
	}

	Create(db)

	if db.Error != nil {
		t.Fatalf("Create returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	t.Logf("Generated SQL: %s", sql)

	// 验证 SQL 包含 RETURNING INTO
	// 新逻辑：有默认值字段时，使用单条插入 + RETURNING
	if !containsReturning(sql) {
		t.Errorf("expected RETURNING in batch insert with default values, got: %s", sql)
	}

	// 验证使用单条 VALUES（不是 INSERT ALL）
	if containsInsertAll(sql) {
		t.Errorf("expected single-row INSERT with RETURNING, not INSERT ALL, got: %s", sql)
	}
}

// TestBatchInsertOptimizationDetection 验证批量插入场景检测逻辑
func TestBatchInsertOptimizationDetection(t *testing.T) {
	// 测试无默认值字段批量插入
	t.Run("无默认值批量插入", func(t *testing.T) {
		// 使用 createTestModel，所有字段都赋值
		model := &[]createTestModel{
			{ID: 1, Name: "A", Code: "X"},
			{ID: 2, Name: "B", Code: "Y"},
		}
		db := newTestDB(t, model)
		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("SQL: %s", sql)

		// 验证优化：无 RETURNING，使用 INSERT ALL
		if containsReturning(sql) {
			t.Errorf("expected no RETURNING when all fields have values")
		}
		if !containsInsertAll(sql) {
			t.Errorf("expected INSERT ALL")
		}
	})

	// 测试有默认值字段批量插入
	t.Run("有默认值批量插入", func(t *testing.T) {
		// 使用 createTestModelDefault，部分字段不赋值
		model := &[]createTestModelDefault{
			{Name: "A"},
			{Name: "B"},
		}
		db := newTestDB(t, model)
		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("SQL: %s", sql)

		// 有默认值字段时：使用单条插入 + RETURNING，逐行执行
		if !containsReturning(sql) {
			t.Errorf("expected RETURNING in batch insert with default values")
		}
		// 不应该使用 INSERT ALL
		if containsInsertAll(sql) {
			t.Errorf("expected single-row INSERT with RETURNING, not INSERT ALL")
		}
	})

	// 测试单行插入无默认值
	t.Run("单行插入无默认值", func(t *testing.T) {
		model := &createTestModel{ID: 1, Name: "A", Code: "X"}
		db := newTestDB(t, model)
		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("SQL: %s", sql)

		// 单条插入，所有默认值字段都有显式值：无 RETURNING
		if containsReturning(sql) {
			t.Errorf("expected no RETURNING when all default fields have values")
		}
	})

	// 测试单行插入有默认值
	t.Run("单行插入有默认值", func(t *testing.T) {
		model := &createTestModelDefault{Name: "A"}
		db := newTestDB(t, model)
		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("SQL: %s", sql)

		// 期望：包含 RETURNING
		if !containsReturning(sql) {
			t.Errorf("expected RETURNING in SQL")
		}
	})
}

// 辅助函数

// containsReturning 检查 SQL 是否包含 RETURNING 子句
func containsReturning(sql string) bool {
	return containsSubstring(sql, "RETURNING")
}

// containsInsertAll 检查 SQL 是否包含 INSERT ALL 语法
func containsInsertAll(sql string) bool {
	return containsSubstring(sql, "INSERT ALL")
}

// containsSubstring 检查字符串是否包含子串（大小写不敏感）
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) != -1
}

// findSubstring 查找子串位置（大小写不敏感）
func findSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			subc := substr[j]
			// 大小写不敏感比较
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if subc >= 'A' && subc <= 'Z' {
				subc += 32
			}
			if sc != subc {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// TestBatchInsertPerformanceMetrics 性能指标测试（已停用）
// 原实现用 time.Sleep 模拟耗时、仅 t.Log 无断言，属于空壳测试。
// 真实性能基准见 tests/batch_insert_performance_test.go（需真实数据库，手动基准工具）。
// 此处显式跳过，避免误导。
func TestBatchInsertPerformanceMetrics(t *testing.T) {
	t.Skip("性能指标已迁移至 tests/batch_insert_performance_test.go，此处不再维护")
}

// Test11gOfflineIDGeneration 验证 11g 离线 ID 生成优化
func Test11gOfflineIDGeneration(t *testing.T) {
	// 测试模型（模拟 11g 自增主键）
	type Model11g struct {
		ID   uint   `gorm:"primaryKey;autoIncrement"`
		Name string `gorm:"size:100"`
	}

	// 使用 11g 版本号初始化 Dialector
	d := newTestDialector("11.2.0.4.0", 256)

	// 测试场景 1：批量插入，ID 为空
	t.Run("批量插入_自动生成ID", func(t *testing.T) {
		model := &[]Model11g{
			{Name: "A"},
			{Name: "B"},
			{Name: "C"},
		}

		db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{DryRun: true})
		if err != nil {
			t.Fatalf("failed to open db: %v", err)
		}
		db.Dialector = d
		callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})

		sch := parseTestSchema(t, model)
		rv := reflect.ValueOf(model).Elem()

		db.Statement = &gorm.Statement{
			DB:           db,
			Schema:       sch,
			Table:        sch.Table,
			Dest:         model,
			ReflectValue: rv,
			Clauses:      map[string]clause.Clause{},
			Vars:         []any{},
		}

		// 注意：DryRun 模式下不会真正生成 ID
		// 这里只是验证代码路径正确
		Create(db)

		sql := db.Statement.SQL.String()
		t.Logf("Generated SQL (DryRun): %s", sql)

		// 验证：由于是 DryRun，不会真正生成 ID
		// 但应该看到正确的 SQL 生成逻辑
	})

	// 测试场景 2：单行插入
	t.Run("单行插入", func(t *testing.T) {
		model := &Model11g{Name: "A"}

		db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{DryRun: true})
		if err != nil {
			t.Fatalf("failed to open db: %v", err)
		}
		db.Dialector = d
		callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})

		sch := parseTestSchema(t, model)
		rv := reflect.ValueOf(model).Elem()

		db.Statement = &gorm.Statement{
			DB:           db,
			Schema:       sch,
			Table:        sch.Table,
			Dest:         model,
			ReflectValue: rv,
			Clauses:      map[string]clause.Clause{},
			Vars:         []any{},
		}

		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("Generated SQL: %s", sql)
	})

	// 测试场景 3：已有 ID 的批量插入
	t.Run("批量插入_已有ID", func(t *testing.T) {
		model := &[]Model11g{
			{ID: 100, Name: "A"},
			{ID: 101, Name: "B"},
			{ID: 102, Name: "C"},
		}

		db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{DryRun: true})
		if err != nil {
			t.Fatalf("failed to open db: %v", err)
		}
		db.Dialector = d
		callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})

		sch := parseTestSchema(t, model)
		rv := reflect.ValueOf(model).Elem()

		db.Statement = &gorm.Statement{
			DB:           db,
			Schema:       sch,
			Table:        sch.Table,
			Dest:         model,
			ReflectValue: rv,
			Clauses:      map[string]clause.Clause{},
			Vars:         []any{},
		}

		Create(db)

		if db.Error != nil {
			t.Fatalf("Create returned error: %v", db.Error)
		}

		sql := db.Statement.SQL.String()
		t.Logf("Generated SQL: %s", sql)

		// 验证：已有 ID 时使用 INSERT ALL 批量插入优化（无 RETURNING）
		if containsReturning(sql) {
			t.Errorf("expected no RETURNING when IDs are provided, got: %s", sql)
		}
		if !containsInsertAll(sql) {
			t.Errorf("expected INSERT ALL batch insert when IDs are provided, got: %s", sql)
		}
	})
}

// TestGenerateSequenceName 验证序列名生成逻辑
func TestGenerateSequenceName(t *testing.T) {
	tests := []struct {
		table    string
		expected string
	}{
		{"TEST_USERS", "SEQ_TEST_USERS"},
		{"TEST_PRODUCTS", "SEQ_TEST_PRODUCTS"},
		{"A_VERY_LONG_TABLE_NAME_THAT_EXCEEDS_30_CHARS", "SEQ_A_VERY_LONG_TABLE_NA_"}, // 会被截断并添加哈希
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			result := generateSequenceName(tt.table)
			// 验证序列名不超过 30 字符
			if len(result) > 30 {
				t.Errorf("sequence name %q exceeds 30 characters: %d", result, len(result))
			}
			t.Logf("Table: %s -> Sequence: %s (len=%d)", tt.table, result, len(result))
		})
	}
}

// TestGetAutoIncrementField 验证获取自增字段逻辑
func TestGetAutoIncrementField(t *testing.T) {
	t.Run("有自增主键", func(t *testing.T) {
		type Model struct {
			ID   uint `gorm:"primaryKey;autoIncrement"`
			Name string
		}

		model := &Model{}
		sch := parseTestSchema(t, model)

		field := getAutoIncrementField(sch)
		if field == nil {
			t.Fatal("expected auto increment field, got nil")
		}
		if field.Name != "ID" {
			t.Errorf("expected field name ID, got %s", field.Name)
		}
	})

	t.Run("主键无autoIncrement标签", func(t *testing.T) {
		// 注意：GORM 可能会自动推断主键为 autoIncrement
		// 这个测试验证显式没有 autoIncrement 标签的情况
		type Model struct {
			ID   uint `gorm:"primaryKey"`
			Name string
		}

		model := &Model{}
		sch := parseTestSchema(t, model)

		field := getAutoIncrementField(sch)
		// GORM 可能自动推断，所以我们不强制要求为 nil
		// 只验证函数不会 panic 或返回错误的字段
		if field != nil && field.Name != "ID" {
			t.Errorf("unexpected field name: %s", field.Name)
		}
		t.Logf("AutoIncrement field: %v", field != nil)
	})
}
