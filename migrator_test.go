package oracle

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// noopDialector 内嵌真实 Dialector 但跳过 Initialize 的数据库连接，
// 用于在无需真实连接的情况下构造合法的 *gorm.DB（cacheStore 会被 gorm.Open 初始化）。
type noopDialector struct {
	Dialector
}

func (noopDialector) Initialize(db *gorm.DB) error {
	return nil
}

// relParent 含关联关系的模型，用于测试 TryRemoveOnUpdate
type relParent struct {
	ID   uint     `gorm:"primaryKey"`
	Kids []relKid `gorm:"foreignKey:ParentID;constraint:OnUpdate:CASCADE"`
}

type relKid struct {
	ID       uint `gorm:"primaryKey"`
	ParentID uint
}

// newTestMigrator 构造一个不依赖真实连接的 Migrator
func newTestMigrator() Migrator {
	return newTestMigratorWithVer("12.1.0.2.0")
}

// newTestMigratorWithVer 构造指定版本的 Migrator
func newTestMigratorWithVer(dbVer string) Migrator {
	d := &Dialector{Config: &Config{DBVer: dbVer, DefaultStringSize: 1024}}
	db, err := gorm.Open(noopDialector{Dialector: *d}, &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return Migrator{Migrator: migrator.Migrator{Config: migrator.Config{
		DB:                          db,
		Dialector:                   d,
		CreateIndexAfterCreateTable: true,
	}}}
}

// TestTryRemoveOnUpdate 验证从关系约束标签中移除 ON UPDATE 片段
func TestTryRemoveOnUpdate(t *testing.T) {
	m := newTestMigrator()

	if err := m.TryRemoveOnUpdate(&relParent{}); err != nil {
		t.Fatalf("TryRemoveOnUpdate returned error: %v", err)
	}
}

// TestTryRemoveOnUpdateWithoutRelations 验证无关联关系的模型不报错
func TestTryRemoveOnUpdateWithoutRelations(t *testing.T) {
	m := newTestMigrator()

	if err := m.TryRemoveOnUpdate(&limitModel{}); err != nil {
		t.Fatalf("TryRemoveOnUpdate returned error: %v", err)
	}
}

// TestRemoveOnUpdateFromConstraint 验证从 CONSTRAINT 标签值中大小写不敏感移除 OnUpdate 子项。
// GORM ParseTagSetting 只大写化 key、保留值原文（如 "OnUpdate:CASCADE,OnDelete:SET NULL"），
// 旧实现用字符串替换 "ON UPDATE xxx" 永不命中，此处按 "," 拆分逐项 EqualFold 匹配。
func TestRemoveOnUpdateFromConstraint(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "OnUpdate:CASCADE", want: ""},
		{in: "OnUpdate:CASCADE,OnDelete:SET NULL", want: "OnDelete:SET NULL"},
		{in: "onupdate:cascade,OnDelete:SET NULL", want: "OnDelete:SET NULL"},
		{in: "OnDelete:SET NULL,OnUpdate:CASCADE", want: "OnDelete:SET NULL"},
		{in: "OnUpdate:CASCADE, OnDelete:SET NULL", want: " OnDelete:SET NULL"},
		{in: "OnDelete:CASCADE", want: "OnDelete:CASCADE"},
		{in: "OnUpdate:RESTRICT,OnUpdate:CASCADE", want: ""},
	}
	for _, c := range cases {
		if got := removeOnUpdateFromConstraint(c.in); got != c.want {
			t.Errorf("removeOnUpdateFromConstraint(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTryRemoveOnUpdateClearsOnUpdate 验证 TryRemoveOnUpdate 会真正清除
// CONSTRAINT 标签中的 OnUpdate 子项（旧实现字符串替换永不命中，
// 导致建表 DDL 残留 ON UPDATE 子句报 ORA-00907）。
func TestTryRemoveOnUpdateClearsOnUpdate(t *testing.T) {
	m := newTestMigrator()

	if err := m.TryRemoveOnUpdate(&relParent{}); err != nil {
		t.Fatalf("TryRemoveOnUpdate returned error: %v", err)
	}

	stmt := &gorm.Statement{DB: m.DB}
	if err := stmt.Parse(&relParent{}); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	for _, rel := range stmt.Schema.Relationships.Relations {
		if got := rel.Field.TagSettings["CONSTRAINT"]; strings.Contains(strings.ToUpper(got), "ONUPDATE") {
			t.Errorf("CONSTRAINT tag %q still contains OnUpdate after TryRemoveOnUpdate", got)
		}
	}
}

// TestQuoteToReservedWords 验证保留字列名在 SQL 输出时被引号包裹（由 QuoteTo 处理）。
// 注意：此前 TryQuotifyReservedWords 会直接修改缓存的 schema（DBNames/DBName），
// 导致 FieldsByDBName 的 key 与 DBName 不一致，gorm CreateTable 时字段查找为 nil 而 panic。
// 正确做法是依赖 QuoteTo 在输出层处理保留字，不污染共享 schema。
func TestQuoteToReservedWords(t *testing.T) {
	d := newTestDialector("11.2.0.4.0", 2000)

	var buf strings.Builder
	d.QuoteTo(&buf, "SELECT") // 保留字 → 引号包裹
	if got := buf.String(); got != `"SELECT"` {
		t.Errorf("QuoteTo(SELECT) = %q, want %q", got, `"SELECT"`)
	}

	buf.Reset()
	d.QuoteTo(&buf, "NAME") // 非保留字 → 原样输出
	if got := buf.String(); got != "NAME" {
		t.Errorf("QuoteTo(NAME) = %q, want %q", got, "NAME")
	}
}

// TestSequenceAndTriggerNaming 验证序列/触发器名称规则
func TestSequenceAndTriggerNaming(t *testing.T) {
	m := newTestMigrator()

	seq := m.sequenceName("TEST_USERS")
	if seq != "SEQ_TEST_USERS" {
		t.Errorf("sequenceName() = %q, want %q", seq, "SEQ_TEST_USERS")
	}

	trg := m.triggerName("TEST_USERS")
	if trg != "TRG_TEST_USERS" {
		t.Errorf("triggerName() = %q, want %q", trg, "TRG_TEST_USERS")
	}
}

// TestMigratorDelegateMethods 验证 Migrator 的 DataTypeOf 委托
func TestMigratorDelegateMethods(t *testing.T) {
	m := newTestMigrator()

	f := testField(schema.Int)
	f.Size = 64
	f.FieldType = reflect.TypeFor[int]()
	f.IndirectFieldType = f.FieldType

	if got := m.DataTypeOf(f); got != "INTEGER" {
		t.Errorf("Migrator.DataTypeOf() = %q, want %q", got, "INTEGER")
	}
}

// TestNoopDialectorImplementsGormDialector 编译期验证 noopDialector 实现了 gorm.Dialector
var _ gorm.Dialector = (*noopDialector)(nil)

// ---------- oracleDBVer ----------

func TestOracleDBVer(t *testing.T) {
	t.Run("pointer dialector 返回版本", func(t *testing.T) {
		m := newTestMigratorWithVer("11.2.0.4.0")
		if got := m.oracleDBVer(); got != "11.2.0.4.0" {
			t.Errorf("oracleDBVer() = %q, want %q", got, "11.2.0.4.0")
		}
	})

	t.Run("空版本返回空串", func(t *testing.T) {
		m := newTestMigratorWithVer("")
		if got := m.oracleDBVer(); got != "" {
			t.Errorf("oracleDBVer() = %q, want empty", got)
		}
	})

	t.Run("非 Dialector 返回空串", func(t *testing.T) {
		db, _ := gorm.Open(noopDialector{Dialector: Dialector{Config: &Config{}}}, &gorm.Config{})
		// 构造一个 Dialector 值类型（非指针），Migrator 的 Dialector 字段存的是值
		m := Migrator{Migrator: migrator.Migrator{Config: migrator.Config{
			DB:        db,
			Dialector: Dialector{Config: &Config{DBVer: "19.0"}},
		}}}
		// Dialector 是值类型，类型断言 *Dialector 会失败，因为实际存储的是 Dialector 值
		if got := m.oracleDBVer(); got != "" {
			t.Errorf("oracleDBVer() for non-pointer dialector = %q, want empty", got)
		}
	})
}

// ---------- hasNEXTVALDefault ----------

func TestHasNEXTVALDefault(t *testing.T) {
	cases := []struct {
		name  string
		field *schema.Field
		want  bool
	}{
		{"nil field", nil, false},
		{"empty default", &schema.Field{DefaultValue: ""}, false},
		{"NEXTVAL 大写", &schema.Field{DefaultValue: "SEQ_X.NEXTVAL"}, true},
		{"nextval 小写", &schema.Field{DefaultValue: "seq_x.nextval"}, true},
		{"Nextval 混合", &schema.Field{DefaultValue: "SEQ_X.Nextval"}, true},
		{"含括号", &schema.Field{DefaultValue: "(SEQ_X.NEXTVAL)"}, true},
		{"普通默认值", &schema.Field{DefaultValue: "hello"}, false},
		{"NEXTVAL 无点前缀", &schema.Field{DefaultValue: "NEXTVAL"}, false},
		{"仅点号", &schema.Field{DefaultValue: ".NEXTVAL"}, true},
		{"含空格", &schema.Field{DefaultValue: " SEQ_X .NEXTVAL "}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasNEXTVALDefault(c.field); got != c.want {
				t.Errorf("hasNEXTVALDefault() = %v, want %v", got, c.want)
			}
		})
	}
}

// ---------- extractSequenceNameFromDefault ----------

func TestExtractSequenceNameFromDefault(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"简单格式", "SEQ_MY.NEXTVAL", "SEQ_MY"},
		{"小写", "seq_my.nextval", "seq_my"},
		{"含括号", "(SEQ_MY.NEXTVAL)", "SEQ_MY"},
		{"含空格", " SEQ_MY .NEXTVAL ", "SEQ_MY"},
		{"空串", "", ""},
		{"无 NEXTVAL", "hello", ""},
		{"仅 NEXTVAL", ".NEXTVAL", ""},
		{"复合名", "SCHEMA.SEQ_MY.NEXTVAL", "SCHEMA.SEQ_MY"},
		{"前导括号无后缀", "(SEQ_MY", ""},
		{"后缀括号无前导", "SEQ_MY)", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractSequenceNameFromDefault(c.input); got != c.want {
				t.Errorf("extractSequenceNameFromDefault(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

// ---------- sequenceName / triggerName 边界 ----------

func TestSequenceAndTriggerNamingLongName(t *testing.T) {
	m := newTestMigrator()

	// 30 字符的表名：SEQ_ + 30 = 34 > 30 → 走哈希分支
	longTable := "ABCDEFGHIJKLMNOPQRSTUVWX" // 24 chars → SEQ_24 = 28 <= 30
	if got := m.sequenceName(longTable); len(got) > 30 {
		t.Errorf("sequenceName(%q) 长度 %d > 30", longTable, len(got))
	}

	// 25 字符的表名：SEQ_ + 25 = 29 ≤ 30 → 不超，走原路径
	justRight := "ABCDEFGHIJKLMNOPQRSTUVWXY" // 25 chars → SEQ_25 = 29
	if got := m.sequenceName(justRight); len(got) != 29 {
		t.Errorf("sequenceName(%q) 长度 %d, want 29", justRight, len(got))
	}

	// 27 字符的表名：SEQ_ + 27 = 31 > 30 → 哈希截断
	tooLong := "ABCDEFGHIJKLMNOPQRSTUVWXYZA" // 27 chars → SEQ_27 = 31 > 30
	if got := m.sequenceName(tooLong); len(got) != 30 {
		t.Errorf("sequenceName(%q) 长度 %d, want 30", tooLong, len(got))
	}

	// 空表名
	if got := m.sequenceName(""); got != "SEQ_" {
		t.Errorf("sequenceName(\"\") = %q, want SEQ_", got)
	}

	// triggerName 同理
	if got := m.triggerName(tooLong); len(got) != 30 {
		t.Errorf("triggerName(%q) 长度 %d, want 30", tooLong, len(got))
	}
	if got := m.triggerName(""); got != "TRG_" {
		t.Errorf("triggerName(\"\") = %q, want TRG_", got)
	}
}

// ---------- validateOracleIdentifier ----------

func TestValidateOracleIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"合法简单", "hello", false},
		{"合法含下划线", "my_table", false},
		{"合法含数字", "table123", false},
		{"合法含$", "my$table", false},
		{"合法含#", "my#table", false},
		{"首字符下划线", "_test", false},
		{"首字符大写", "Test", false},
		{"29字符合法", "A1234567890123456789012345678", false}, // 29 chars
		{"空串", "", true},
		{"超30字符", "A1234567890123456789012345678901", true}, // 31 chars
		{"首字符数字", "1table", true},
		{"含空格", "my table", true},
		{"含连字符", "my-table", true},
		{"含点号", "my.table", true},
		{"含中文", "表名", true},
		{"含双引号", `"table"`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateOracleIdentifier(c.input)
			if (err != nil) != c.wantErr {
				t.Errorf("validateOracleIdentifier(%q) error = %v, wantErr %v", c.input, err, c.wantErr)
			}
		})
	}
}

// ---------- onUpdateTriggerName ----------

func TestOnUpdateTriggerName(t *testing.T) {
	t.Run("短名称不截断", func(t *testing.T) {
		got := onUpdateTriggerName("orders", "user_id", "id")
		want := "fk_trigger_orders_user_id_id"
		if got != want {
			t.Errorf("onUpdateTriggerName() = %q, want %q", got, want)
		}
		if len(got) > 30 {
			t.Errorf("长度 %d > 30", len(got))
		}
	})

	t.Run("超长名称哈希截断", func(t *testing.T) {
		got := onUpdateTriggerName("very_long_table_name_here", "extremely_long_fk_column_name", "id")
		if len(got) > 30 {
			t.Errorf("onUpdateTriggerName() 长度 %d > 30", len(got))
		}
	})

	t.Run("空参数", func(t *testing.T) {
		got := onUpdateTriggerName("", "", "")
		want := "fk_trigger___"
		if got != want {
			t.Errorf("onUpdateTriggerName(\"\") = %q, want %q", got, want)
		}
	})
}

// ---------- FullDataTypeOf ----------

func TestMigratorFullDataTypeOf(t *testing.T) {
	m := newTestMigrator()

	t.Run("基础类型", func(t *testing.T) {
		f := testField(schema.Int)
		f.Size = 64
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m.FullDataTypeOf(f)
		if got.SQL != "INTEGER" {
			t.Errorf("FullDataTypeOf(int64) = %q, want INTEGER", got.SQL)
		}
	})

	t.Run("NOT NULL", func(t *testing.T) {
		f := testField(schema.Int)
		f.Size = 64
		f.NotNull = true
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m.FullDataTypeOf(f)
		if !strings.Contains(got.SQL, "NOT NULL") {
			t.Errorf("FullDataTypeOf(NOT NULL) = %q, want contain NOT NULL", got.SQL)
		}
	})

	t.Run("NEXTVAL 默认值 12c", func(t *testing.T) {
		m12 := newTestMigratorWithVer("12.1.0.2.0")
		f := testField(schema.Int)
		f.Size = 64
		f.HasDefaultValue = true
		f.DefaultValue = "SEQ_MY.NEXTVAL"
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m12.FullDataTypeOf(f)
		if !strings.Contains(got.SQL, "DEFAULT") {
			t.Errorf("FullDataTypeOf(12c NEXTVAL) = %q, want contain DEFAULT", got.SQL)
		}
	})

	t.Run("NEXTVAL 默认值 11g 不生成 DEFAULT", func(t *testing.T) {
		m11 := newTestMigratorWithVer("11.2.0.4.0")
		f := testField(schema.Int)
		f.Size = 64
		f.HasDefaultValue = true
		f.DefaultValue = "SEQ_MY.NEXTVAL"
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m11.FullDataTypeOf(f)
		// 11g 下 NEXTVAL 默认值不生成 DEFAULT 子句
		if strings.Contains(got.SQL, "DEFAULT") {
			t.Errorf("FullDataTypeOf(11g NEXTVAL) = %q, 不应包含 DEFAULT", got.SQL)
		}
	})

	t.Run("DefaultValueInterface", func(t *testing.T) {
		f := testField(schema.Int)
		f.Size = 64
		f.HasDefaultValue = true
		f.DefaultValueInterface = int64(42)
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m.FullDataTypeOf(f)
		if !strings.Contains(got.SQL, "DEFAULT") {
			t.Errorf("FullDataTypeOf(DefaultValueInterface) = %q, want contain DEFAULT", got.SQL)
		}
	})

	t.Run("普通默认值", func(t *testing.T) {
		f := testField(schema.String)
		f.Size = 100
		f.HasDefaultValue = true
		f.DefaultValue = "hello"
		f.FieldType = reflect.TypeFor[string]()
		f.IndirectFieldType = f.FieldType
		got := m.FullDataTypeOf(f)
		if !strings.Contains(got.SQL, "DEFAULT") {
			t.Errorf("FullDataTypeOf(普通默认值) = %q, want contain DEFAULT", got.SQL)
		}
	})

	t.Run("(-) 默认值跳过", func(t *testing.T) {
		f := testField(schema.Int)
		f.Size = 64
		f.HasDefaultValue = true
		f.DefaultValue = "(-)"
		f.FieldType = reflect.TypeFor[int]()
		f.IndirectFieldType = f.FieldType
		got := m.FullDataTypeOf(f)
		// "(-)" 是 GORM 的特殊标记，不应生成 DEFAULT 子句
		if strings.Contains(got.SQL, "DEFAULT") {
			t.Errorf("FullDataTypeOf((-)) = %q, 不应包含 DEFAULT", got.SQL)
		}
	})
}

// ---------- AlterDataTypeOf ----------
// AlterDataTypeOf 内部调用 m.DB.Raw().Row().Scan() 查询列的 NULLABLE 属性，
// 依赖真实数据库连接，无法通过 noopDialector 单测覆盖。
// 仅测试 DataTypeOf 委托部分（通过 FullDataTypeOf 间接覆盖）。

// ---------- createOnUpdateTrigger 早期返回 ----------

func TestCreateOnUpdateTriggerNilRel(t *testing.T) {
	m := newTestMigrator()
	err := m.CreateOnUpdateTrigger(nil, nil)
	if err == nil {
		t.Error("CreateOnUpdateTrigger(nil, nil) 应返回错误")
	}
}

func TestDropOnUpdateTriggerNilRel(t *testing.T) {
	m := newTestMigrator()
	err := m.DropOnUpdateTrigger(nil, nil)
	if err == nil {
		t.Error("DropOnUpdateTrigger(nil, nil) 应返回错误")
	}
}

func TestDropOnUpdateTriggerNilFieldSchema(t *testing.T) {
	m := newTestMigrator()
	// rel.FieldSchema == nil 时应直接返回 nil
	rel := &schema.Relationship{
		Field: &schema.Field{},
	}
	err := m.DropOnUpdateTrigger(&limitModel{}, rel)
	if err != nil {
		t.Errorf("DropOnUpdateTrigger(nil FieldSchema) 应返回 nil, got %v", err)
	}
}

func TestCreateOnUpdateTriggerNilConstraint(t *testing.T) {
	m := newTestMigrator()
	// createOnUpdateTrigger 内部在 constraint==nil 时直接返回 nil，
	// 但 CreateOnUpdateTrigger 公开方法会调用 rel.ParseConstraint()，
	// 需要完整 schema 关系才能不 panic，因此这里直接测试 createOnUpdateTrigger
	rel := &schema.Relationship{
		Field: &schema.Field{},
	}
	err := m.createOnUpdateTrigger(&limitModel{}, rel, nil)
	// constraint 为 nil，应返回 nil（不报错）
	if err != nil {
		t.Errorf("createOnUpdateTrigger(nil constraint) 应返回 nil, got %v", err)
	}
}

// ---------- createOnUpdateTrigger 内部逻辑 ----------

func TestCreateOnUpdateTriggerUnsupportedOnUpdate(t *testing.T) {
	m := newTestMigrator()
	constraint := &schema.Constraint{OnUpdate: "NO ACTION"}
	rel := &schema.Relationship{Field: &schema.Field{}}
	err := m.createOnUpdateTrigger(&limitModel{}, rel, constraint)
	if err != nil {
		t.Errorf("createOnUpdateTrigger(unsupported OnUpdate) 应返回 nil, got %v", err)
	}
}

func TestCreateOnUpdateTriggerEmptyOnUpdate(t *testing.T) {
	m := newTestMigrator()
	constraint := &schema.Constraint{OnUpdate: ""}
	rel := &schema.Relationship{Field: &schema.Field{}}
	err := m.createOnUpdateTrigger(&limitModel{}, rel, constraint)
	if err != nil {
		t.Errorf("createOnUpdateTrigger(empty OnUpdate) 应返回 nil, got %v", err)
	}
}

func TestCreateOnUpdateTriggerEmptyFKCol(t *testing.T) {
	m := newTestMigrator()
	constraint := &schema.Constraint{
		OnUpdate:    "CASCADE",
		ForeignKeys: nil,
		References:  []*schema.Field{{DBName: "id"}},
	}
	rel := &schema.Relationship{Field: &schema.Field{DBName: ""}}
	err := m.createOnUpdateTrigger(&limitModel{}, rel, constraint)
	if err != nil {
		t.Errorf("createOnUpdateTrigger(empty FK col) 应返回 nil, got %v", err)
	}
}

func TestCreateOnUpdateTriggerEmptyReferences(t *testing.T) {
	m := newTestMigrator()
	constraint := &schema.Constraint{
		OnUpdate:    "CASCADE",
		ForeignKeys: []*schema.Field{{DBName: "parent_id"}},
		References:  nil,
	}
	rel := &schema.Relationship{Field: &schema.Field{DBName: "parent_id"}}
	err := m.createOnUpdateTrigger(&limitModel{}, rel, constraint)
	if err != nil {
		t.Errorf("createOnUpdateTrigger(empty References) 应返回 nil, got %v", err)
	}
}

// createOnUpdateTrigger 的 CASCADE/SET NULL 分支需要 m.RunWithValue 解析 schema，
// 依赖完整 schema 关系和 DB 连接，标记为集成测试覆盖。

// ---------- GetTypeAliases ----------

func TestMigratorGetTypeAliases(t *testing.T) {
	m := newTestMigrator()
	cases := []struct {
		input string
		want  []string
	}{
		{"number", []string{"integer", "smallint"}},
		{"NUMBER", []string{"integer", "smallint"}},
		{"varchar2", nil},
		{"", nil},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got := m.GetTypeAliases(c.input)
			if len(got) != len(c.want) {
				t.Fatalf("GetTypeAliases(%q) = %v, want %v", c.input, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("GetTypeAliases(%q)[%d] = %q, want %q", c.input, i, got[i], c.want[i])
				}
			}
		})
	}
}

// ---------- CurrentDatabase ----------
// CurrentDatabase 调用 m.DB.Raw().Row().Scan()，依赖真实数据库连接，无法单测。

// ---------- HasTable ----------
// HasTable 调用 m.DB.Raw().Row().Scan()，依赖真实数据库连接，无法单测。

// ---------- HasColumn ----------
// HasColumn 调用 m.DB.Raw().Row().Scan()，依赖真实数据库连接，无法单测。

// ---------- HasConstraint ----------
// HasConstraint 调用 m.DB.Raw().Row().Scan()，依赖真实数据库连接，无法单测。

// ---------- HasIndex ----------
// HasIndex 调用 m.DB.Raw().Row().Scan()，依赖真实数据库连接，无法单测。

// ---------- AddColumn ----------
// AddColumn 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- DropColumn ----------
// DropColumn 调用 m.HasColumn() + m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- AlterColumn ----------
// AlterColumn 调用 m.HasColumn() + m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- DropIndex ----------
// DropIndex 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- RenameIndex ----------
// RenameIndex 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- RenameTable ----------
// RenameTable 调用 m.HasTable() + m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- CreateConstraint ----------
// CreateConstraint 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- DropConstraint ----------
// DropConstraint 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。

// ---------- DropTable ----------
// DropTable 调用 m.DB.Exec()，依赖真实数据库连接，无法单测。
