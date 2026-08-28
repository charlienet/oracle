package oracle

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	gormSchema "gorm.io/gorm/schema"
)

// updateTestModel 含主键与可更新字段，用于 Update 回调 SQL 生成测试
type updateTestModel struct {
	ID   uint `gorm:"primaryKey"`
	Name string
	Age  int
}

// autoTimeModel 含 autoUpdateTime 字段，用于验证 AutoUpdateTime 自动更新语义
type autoTimeModel struct {
	ID        uint `gorm:"primaryKey"`
	Name      string
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func TestUpdateSQLGeneration(t *testing.T) {
	// 设置主键值后应生成 UPDATE ... SET ... WHERE
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"UPDATE", "SET", "WHERE"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Update SQL %q missing %q", sql, want)
		}
	}
}

func TestUpdateMissingWhereError(t *testing.T) {
	// 主键无值且未提供 WHERE 时应报错
	model := &updateTestModel{Name: "bob"}
	db := newTestDB(t, model)

	Update(db)

	if db.Error == nil {
		t.Fatal("expected error for missing WHERE condition, got nil")
	}
	if !strings.Contains(db.Error.Error(), "missing WHERE") {
		t.Errorf("error %q does not contain %q", db.Error.Error(), "missing WHERE")
	}
}

func TestUpdateSelectsStarFullFieldUpdate(t *testing.T) {
	// 回归：Save 的 UPDATE 分支会设置 Selects=["*"]（全字段更新语义），
	// SelectAndOmitColumns 返回所有字段值 true 且 restricted=false，
	// 修复前过滤逻辑把 true 误判为「被 Omit」，跳过全部字段，生成 GORM
	// 空 SET 兜底的 "SET id=id" no-op SQL，导致 Update/Save 静默不更新。
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"*"} // 模拟 Save 全字段 UPDATE 分支

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"name", "age"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Save-style UPDATE SQL %q missing column %q", sql, want)
		}
	}
	if strings.Contains(sql, "id=id") {
		t.Errorf("Save-style UPDATE SQL %q contains no-op SET id=id: 所有字段被误跳过", sql)
	}
}

func TestUpdateSelectRestrictsColumns(t *testing.T) {
	// 显式 Select 指定列时（restricted=true），仅指定列进入 SET
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"name"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "name") {
		t.Errorf("Update SQL %q missing selected column name", sql)
	}
	if strings.Contains(sql, "age") {
		t.Errorf("Update SQL %q should not contain omitted-by-select column age", sql)
	}
}

func TestUpdateOmitExcludesColumn(t *testing.T) {
	// Omit 指定列时（restricted=false），被 Omit 的列不进入 SET，其余列正常更新
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)
	db.Statement.Omits = []string{"age"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if strings.Contains(sql, "age") {
		t.Errorf("Update SQL %q should not contain omitted column age", sql)
	}
	if !strings.Contains(sql, "name") {
		t.Errorf("Update SQL %q missing non-omitted column name", sql)
	}
}

func TestUpdateMapBranchFullFieldUpdate(t *testing.T) {
	// 回归：map 更新分支在 Selects=["*"]（Save 语义）下同样不得跳过全部字段
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "alice", "age": 30}
	db.Statement.Selects = []string{"*"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"name", "age"} {
		if !strings.Contains(sql, want) {
			t.Errorf("map UPDATE SQL %q missing column %q", sql, want)
		}
	}
	if strings.Contains(sql, "id=id") {
		t.Errorf("map UPDATE SQL %q contains no-op SET id=id: 所有字段被误跳过", sql)
	}
}

func TestUpdateSaveIncludesZeroValueFields(t *testing.T) {
	// 回归：Save 的 UPDATE 分支设置 Selects=["*"]（GORM 官方全字段含零值更新语义），
	// 零值字段（Name 空串、Age 0）也必须进入 SET——修复前无条件 !isZero 过滤会跳过它们
	model := &updateTestModel{ID: 1, Age: 0}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"*"} // Save 语义：全字段更新

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"name", "age"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Save-style UPDATE SQL %q 应包含零值字段 %q（全字段更新语义）", sql, want)
		}
	}
	if strings.Contains(sql, "id=id") {
		t.Errorf("Save-style UPDATE SQL %q contains no-op SET id=id", sql)
	}
}

func TestUpdateSelectIncludesZeroValue(t *testing.T) {
	// 显式 Select 的列（restricted=true）零值也更新
	model := &updateTestModel{ID: 1, Age: 0}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"age"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "age") {
		t.Errorf("UPDATE SQL %q 应包含显式 Select 的零值列 age", sql)
	}
	if strings.Contains(sql, "name") {
		t.Errorf("UPDATE SQL %q 不应包含未选中的列 name", sql)
	}
}

func TestUpdateStructSkipsZeroWithoutSelect(t *testing.T) {
	// GORM Updates(struct) 语义：无显式 Select 时零值字段跳过
	model := &updateTestModel{ID: 1, Name: "alice"} // Age 为零值
	db := newTestDB(t, model)

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "name") {
		t.Errorf("UPDATE SQL %q 应包含非零字段 name", sql)
	}
	if strings.Contains(sql, "age") {
		t.Errorf("UPDATE SQL %q 不应包含零值字段 age（无显式选择时零值跳过）", sql)
	}
}

func TestUpdateMapAllKeysInSet(t *testing.T) {
	// GORM map 语义：map 更新无零值过滤，所有键（含零值）都进 SET
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "alice", "age": 0}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	for _, want := range []string{"name", "age"} {
		if !strings.Contains(sql, want) {
			t.Errorf("map UPDATE SQL %q 应包含键 %q（map 更新零值也进 SET）", sql, want)
		}
	}
}

func TestUpdateMapUnknownKeyAsColumn(t *testing.T) {
	// GORM map 语义：无对应 field 的键按原列名直接进 SET
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "alice", "custom_col": 1}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "custom_col") {
		t.Errorf("map UPDATE SQL %q 应包含无 field 的原始键 custom_col", sql)
	}
}

func TestUpdateAutoUpdateTime(t *testing.T) {
	// struct 更新时 AutoUpdateTime 字段在主循环内按类型生成并进入 SET
	model := &autoTimeModel{ID: 1, Name: "x"}
	db := newTestDB(t, model)

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "updated_at") {
		t.Errorf("UPDATE SQL %q 应包含 autoUpdateTime 字段 updated_at", sql)
	}

	// 绑定值应为 time.Time（AutoUpdateTime 默认类型生成 now）
	foundTime := false
	for _, v := range db.Statement.Vars {
		if _, ok := v.(time.Time); ok {
			foundTime = true
			break
		}
	}
	if !foundTime {
		t.Errorf("UPDATE Vars %v 中应存在 time.Time 类型的 updated_at 绑定值", db.Statement.Vars)
	}
}

func TestUpdateOmitWithSelectStar(t *testing.T) {
	// Save 语义（Selects=["*"]）下 Omit 指定列应被排除，其余列全部更新
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"*"} // Save 语义：全字段更新
	db.Statement.Omits = []string{"name"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "age") {
		t.Errorf("UPDATE SQL %q 应包含未被 Omit 的列 age", sql)
	}
	if strings.Contains(sql, "name") {
		t.Errorf("UPDATE SQL %q 不应包含被 Omit 的列 name", sql)
	}
}

func TestUpdateOmitWithMap(t *testing.T) {
	// map 更新配合 Omit：被 Omit 的键不进 SET，其余键正常更新
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "alice", "age": 30}
	db.Statement.Omits = []string{"age"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "name") {
		t.Errorf("map UPDATE SQL %q 应包含未被 Omit 的键 name", sql)
	}
	if strings.Contains(sql, "age") {
		t.Errorf("map UPDATE SQL %q 不应包含被 Omit 的键 age", sql)
	}
}

func TestUpdateOmitOverridesSelect(t *testing.T) {
	// Omit 与 Select 同列时 Omit 优先（GORM 语义），该列不进 SET
	model := &updateTestModel{ID: 1, Name: "alice"}
	db := newTestDB(t, model)
	db.Statement.Selects = []string{"name"}
	db.Statement.Omits = []string{"name"}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if strings.Contains(sql, "name") {
		t.Errorf("UPDATE SQL %q 不应包含被 Omit 覆盖的列 name", sql)
	}
}

func TestUpdateExprValue(t *testing.T) {
	// gorm.Expr 表达式更新：SQL 中保留表达式，绑定参数进入 Vars
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"age": gorm.Expr("age + ?", 1)}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "age + ") {
		t.Errorf("UPDATE SQL %q 应包含表达式 age + ? 的 SQL 片段", sql)
	}
	// 表达式绑定参数（值 1）应进入 Vars（主键 id=1 之外另有 expr 的 1）
	found := false
	for _, v := range db.Statement.Vars {
		if vv, ok := v.(int); ok && vv == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UPDATE Vars %v 中应包含表达式绑定值 1", db.Statement.Vars)
	}
}

func TestUpdateSubQueryValue(t *testing.T) {
	// 子查询更新：map 值传 *gorm.DB 时应对齐 GORM 官方包装为 []interface{}{kv}，
	// 生成带括号的 name=(SELECT ...)，否则 Oracle 语法非法（name=SELECT）
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	subDB := db.Statement.DB.Session(&gorm.Session{DryRun: true}).Model(&updateTestModel{}).Select("name").Where("id = ?", 5)
	db.Statement.Dest = map[string]any{"name": subDB}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if !strings.Contains(sql, "name=(SELECT") {
		t.Errorf("子查询更新应生成带括号的 name=(SELECT ...)，实际 SQL: %q", sql)
	}
}

func TestStatementChanged(t *testing.T) {
	// Statement.Changed 独立对比 Dest 与 ReflectValue，不依赖 ConvertToAssignments
	model := &updateTestModel{ID: 1, Name: "alice", Age: 30}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "bob"}

	if !db.Statement.Changed("name") {
		t.Errorf("Changed(\"name\") 应为 true：map 值 bob 与 ReflectValue 的 alice 不同")
	}
	if db.Statement.Changed("age") {
		t.Errorf("Changed(\"age\") 应为 false：map 未提供 age")
	}

	// 值相同时 Changed 应为 false
	db.Statement.Dest = map[string]any{"name": "alice"}
	if db.Statement.Changed("name") {
		t.Errorf("Changed(\"name\") 应为 false：map 值 alice 与 ReflectValue 的 alice 相同")
	}
}

func TestUpdateMapLowercaseDBNameKey(t *testing.T) {
	// 回归：map 用小写 DBName 键（如 "name"）更新时，LookUpField 无法命中
	// （本驱动 Namer 将 DBName 大写化为 "NAME"），需大小写不敏感回退命中 schema 字段，
	// SET 列名统一用 field.DBName，且每个列只出现一次（无 ORA-00957 重复列隐患）。
	model := &updateTestModel{ID: 1}
	db := newTestDB(t, model)
	db.Statement.Dest = map[string]any{"name": "alice", "age": 30}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	upper := strings.ToUpper(sql)
	for _, want := range []string{"NAME", "AGE"} {
		if !strings.Contains(upper, want) {
			t.Errorf("map UPDATE SQL %q 应包含大小写不敏感列 %q", sql, want)
		}
	}
	// EqualFold 回退命中后列名统一为 field.DBName，每列在 SET 中只出现一次
	for _, col := range []string{"NAME", "AGE"} {
		if n := strings.Count(upper, col); n != 1 {
			t.Errorf("map UPDATE SQL %q 中列 %s 出现 %d 次，应恰 1 次（无重复列）", sql, col, n)
		}
	}
}

func TestUpdateMapLowercaseNoDuplicateAutoTime(t *testing.T) {
	// 回归：map 用小写键 "updated_at" 提供 AutoUpdateTime 字段时，
	// 回退命中 + AutoUpdateTime 去重（大小写不敏感判断"已提供"），
	// SET 中该列只出现一次（修复前追加 UPDATED_AT 与 updated_at 重复，ORA-00957 隐患）。
	model := &autoTimeModel{ID: 1}
	db := newTestDB(t, model)
	someTime := time.Now()
	db.Statement.Dest = map[string]any{"updated_at": someTime}

	Update(db)

	if db.Error != nil {
		t.Fatalf("Update returned error: %v", db.Error)
	}

	sql := db.Statement.SQL.String()
	if n := strings.Count(strings.ToUpper(sql), "UPDATED_AT"); n != 1 {
		t.Errorf("map UPDATE SQL %q 中列 UPDATED_AT 出现 %d 次，应恰 1 次（无 ORA-00957 隐患）", sql, n)
	}

	// 绑定值应包含用户提供的 someTime（而非补充的当前时间）
	found := false
	for _, v := range db.Statement.Vars {
		if tv, ok := v.(time.Time); ok && tv.Equal(someTime) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("UPDATE Vars %v 中应包含用户提供的 updated_at 值 %v", db.Statement.Vars, someTime)
	}

	// 场景 2：map 未提供 updated_at 时，AutoUpdateTime 补充恰一次
	db2 := newTestDB(t, &autoTimeModel{ID: 1})
	db2.Statement.Dest = map[string]any{"name": "bob"}

	Update(db2)

	if db2.Error != nil {
		t.Fatalf("Update returned error: %v", db2.Error)
	}
	sql2 := db2.Statement.SQL.String()
	if n := strings.Count(strings.ToUpper(sql2), "UPDATED_AT"); n != 1 {
		t.Errorf("map UPDATE SQL %q 中列 UPDATED_AT 出现 %d 次，应恰 1 次（AutoUpdateTime 补充）", sql2, n)
	}
}

// ---- autoUpdateTimeValue 测试 ----

func TestAutoUpdateTimeValue(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	db := &gorm.DB{Config: &gorm.Config{NowFunc: func() time.Time { return now }}}
	stmt := &gorm.Statement{DB: db}

	tests := []struct {
		name       string
		autoUpdate gormSchema.TimeType
		expectType string
	}{
		{"default (time.Time)", 0, "time.Time"},
		{"UnixNanosecond", gormSchema.UnixNanosecond, "int64"},
		{"UnixMillisecond", gormSchema.UnixMillisecond, "int64"},
		{"UnixSecond", gormSchema.UnixSecond, "int64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := &gormSchema.Field{AutoUpdateTime: tt.autoUpdate}
			got := autoUpdateTimeValue(stmt, field)
			gotType := reflect.TypeOf(got).String()
			if gotType != tt.expectType {
				t.Errorf("autoUpdateTimeValue() returned %T, want %s", got, tt.expectType)
			}
		})
	}
}

func TestAutoUpdateTimeValueUnixNanosecond(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	db := &gorm.DB{Config: &gorm.Config{NowFunc: func() time.Time { return now }}}
	stmt := &gorm.Statement{DB: db}
	field := &gormSchema.Field{AutoUpdateTime: gormSchema.UnixNanosecond}

	got := autoUpdateTimeValue(stmt, field)
	if got != now.UnixNano() {
		t.Errorf("autoUpdateTimeValue() = %v, want %v", got, now.UnixNano())
	}
}

func TestAutoUpdateTimeValueUnixMillisecond(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	db := &gorm.DB{Config: &gorm.Config{NowFunc: func() time.Time { return now }}}
	stmt := &gorm.Statement{DB: db}
	field := &gormSchema.Field{AutoUpdateTime: gormSchema.UnixMillisecond}

	got := autoUpdateTimeValue(stmt, field)
	if got != now.UnixMilli() {
		t.Errorf("autoUpdateTimeValue() = %v, want %v", got, now.UnixMilli())
	}
}

func TestAutoUpdateTimeValueUnixSecond(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	db := &gorm.DB{Config: &gorm.Config{NowFunc: func() time.Time { return now }}}
	stmt := &gorm.Statement{DB: db}
	field := &gormSchema.Field{AutoUpdateTime: gormSchema.UnixSecond}

	got := autoUpdateTimeValue(stmt, field)
	if got != now.Unix() {
		t.Errorf("autoUpdateTimeValue() = %v, want %v", got, now.Unix())
	}
}

// ---- Update 边界情况 ----

func TestUpdateNilStatement(t *testing.T) {
	db := &gorm.DB{Statement: &gorm.Statement{}}
	db.Statement.Schema = nil
	Update(db)
	// 不应 panic
}

func TestUpdateNilSchema(t *testing.T) {
	db := &gorm.DB{Statement: &gorm.Statement{}}
	Update(db)
	// 不应 panic
}
