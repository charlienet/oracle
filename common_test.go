package oracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// ---- 测试辅助类型 ----

// testValuer 实现 driver.Valuer，用于测试 convertValue 的解包逻辑
type testValuer struct {
	value driver.Value
	err   error
}

func (v testValuer) Value() (driver.Value, error) {
	return v.value, v.err
}

// softDeleteModel 含软删除字段，用于构造 schema
type softDeleteModel struct {
	ID        uint `gorm:"primaryKey"`
	DeletedAt gorm.DeletedAt
}

// plainModel 无软删除字段
type plainModel struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

// createDataModel 用于 validateCreateData 测试
type createDataModel struct {
	ID int
}

func parseTestSchema(t *testing.T, model any) *schema.Schema {
	t.Helper()
	sch, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	return sch
}

// ---- TestConvertValue ----

func TestConvertValue(t *testing.T) {
	now := time.Now()
	longString := strings.Repeat("a", 4001)

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{"bool true to 1", true, 1},
		{"bool false to 0", false, 0},
		{"plain string unchanged", "hello", "hello"},
		{"long string (over 4000) unchanged", longString, longString},
		{"nil unchanged", nil, nil},
		{"time.Time unchanged", now, now},
		{"driver.Valuer unwrapped", testValuer{value: int64(42)}, int64(42)},
		{"driver.Valuer with string", testValuer{value: "val"}, "val"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertValue(tt.value, nil)
			if tt.want == nil {
				if got != nil {
					t.Errorf("convertValue(%v) = %v, want nil", tt.value, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("convertValue(%v) = %v (%T), want %v (%T)", tt.value, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestConvertValueValuerError(t *testing.T) {
	v := testValuer{err: errors.New("valuing failed")}
	got := convertValue(v, nil)
	// 出错时返回原始值
	if _, ok := got.(testValuer); !ok {
		t.Errorf("expected original value returned on Valuer error, got %#v", got)
	}
}

// ---- TestConvertFromOracleToField ----

func TestConvertFromOracleToField(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{"nil unchanged", nil, nil},
		{"plain int unchanged", 42, 42},
		{"plain string unchanged", "abc", "abc"},
		{"NullTime valid", sql.NullTime{Time: now, Valid: true}, now},
		{"NullTime invalid", sql.NullTime{Valid: false}, nil},
		{"NullInt64 valid", sql.NullInt64{Int64: 99, Valid: true}, int64(99)},
		{"NullInt64 invalid", sql.NullInt64{Valid: false}, nil},
		{"NullFloat64 valid", sql.NullFloat64{Float64: 3.14, Valid: true}, 3.14},
		{"NullFloat64 invalid", sql.NullFloat64{Valid: false}, nil},
		{"NullBool valid", sql.NullBool{Bool: true, Valid: true}, true},
		{"NullBool invalid", sql.NullBool{Valid: false}, nil},
		{"NullString valid", sql.NullString{String: "x", Valid: true}, "x"},
		{"NullString invalid", sql.NullString{Valid: false}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertFromOracleToField(tt.value, nil)
			if tt.want == nil {
				if got != nil {
					t.Errorf("convertFromOracleToField(%v) = %v, want nil", tt.value, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("convertFromOracleToField(%v) = %v (%T), want %v (%T)", tt.value, got, got, tt.want, tt.want)
			}
		})
	}
}

// ---- TestBuildOracleDefault ----

func TestBuildOracleDefault(t *testing.T) {
	// 12c 及以上版本号（NEXTVAL 默认值走 DEFAULT 子句）
	const dbVer12c = "12.1.0.2.0"
	// 11g 版本号（NEXTVAL 默认值不生成 DEFAULT 子句）
	const dbVer11g = "11.2.0.4.0"

	tests := []struct {
		name     string
		dbVer    string
		value    string
		expected string
	}{
		{"empty string", dbVer12c, "", ""},
		{"NULL keyword", dbVer12c, "NULL", "DEFAULT NULL"},
		{"null lowercase", dbVer12c, "null", "DEFAULT NULL"},
		{"CURRENT_TIMESTAMP", dbVer12c, "CURRENT_TIMESTAMP", "DEFAULT CURRENT_TIMESTAMP"},
		{"now()", dbVer12c, "now()", "DEFAULT CURRENT_TIMESTAMP"},
		{"SYSDATE", dbVer12c, "SYSDATE", "DEFAULT SYSDATE"},
		{"sysdate lowercase", dbVer12c, "sysdate", "DEFAULT SYSDATE"},
		{"TRUE", dbVer12c, "TRUE", "DEFAULT 1"},
		{"false lowercase", dbVer12c, "false", "DEFAULT 0"},
		// 12c+ 原生支持 DEFAULT <seq>.NEXTVAL，行为不变
		{"sequence nextval 12c", dbVer12c, "SEQ_MY.NEXTVAL", "DEFAULT SEQ_MY.NEXTVAL"},
		// 11g 的 DEFAULT 子句不支持引用序列 NEXTVAL（ORA-00984），返回空串，
		// 由建表流程创建 BEFORE INSERT 触发器实现等价语义
		{"sequence nextval 11g", dbVer11g, "SEQ_MY.NEXTVAL", ""},
		{"sequence nextval 11g lowercase", dbVer11g, "seq_my.nextval", ""},
		{"date format", dbVer12c, "2006-01-02", "DEFAULT TO_DATE('2006-01-02', 'YYYY-MM-DD')"},
		{"timestamp format", dbVer12c, "2006-01-02 15:04:05", "DEFAULT TO_DATE('2006-01-02 15:04:05', 'YYYY-MM-DD HH24:MI:SS')"},
		{"plain string", dbVer12c, "hello", "DEFAULT 'hello'"},
		{"plain string with spaces", dbVer12c, "  hello  ", "DEFAULT '  hello  '"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildOracleDefault(tt.dbVer, tt.value, nil)
			if got != tt.expected {
				t.Errorf("buildOracleDefault(%q, %q) = %q, want %q", tt.dbVer, tt.value, got, tt.expected)
			}
		})
	}
}

// ---- TestCheckMissingWhereConditions ----

func TestCheckMissingWhereConditions(t *testing.T) {
	softSch := parseTestSchema(t, &softDeleteModel{})
	plainSch := parseTestSchema(t, &plainModel{})

	softDeleteCond := clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil}
	normalCond := clause.Eq{Column: "age", Value: 25}

	tests := []struct {
		name       string
		conditions []clause.Expression
		schema     *schema.Schema
		want       bool
	}{
		{"empty conditions", nil, softSch, true},
		{"empty slice", []clause.Expression{}, plainSch, true},
		{"only soft delete condition", []clause.Expression{softDeleteCond}, softSch, true},
		{"soft delete + normal condition", []clause.Expression{softDeleteCond, normalCond}, softSch, false},
		{"only normal condition", []clause.Expression{normalCond}, softSch, false},
		{"soft delete condition on model without deleted_at", []clause.Expression{softDeleteCond}, plainSch, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkMissingWhereConditions(tt.conditions, tt.schema)
			if got != tt.want {
				t.Errorf("checkMissingWhereConditions(%v) = %v, want %v", tt.conditions, got, tt.want)
			}
		})
	}
}

// ---- TestValidateCreateData ----

func TestValidateCreateData(t *testing.T) {
	tests := []struct {
		name    string
		data    any
		wantErr string
	}{
		{"nil data", nil, "create data cannot be nil"},
		{"nil pointer", (*createDataModel)(nil), "create data pointer cannot be nil"},
		{"empty slice", []createDataModel{}, "create data slice cannot be empty"},
		{"valid struct", createDataModel{ID: 1}, ""},
		{"valid non-empty slice", []createDataModel{{ID: 1}}, ""},
		{"valid pointer to struct", &createDataModel{ID: 2}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateData(tt.data)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateCreateData(%v) = %v, want nil error", tt.data, err)
				}
				return
			}
			if err == nil {
				t.Errorf("validateCreateData(%v) = nil, want error containing %q", tt.data, tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("validateCreateData(%v) error = %q, want containing %q", tt.data, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSoftDeleteFieldDetection(t *testing.T) {
	// 测试场景 1: gorm.DeletedAt 类型字段
	type Model1 struct {
		ID        uint `gorm:"primaryKey"`
		DeletedAt gorm.DeletedAt
	}
	sch1 := parseTestSchema(t, &Model1{})
	
	// 验证能正确检测到软删除字段
	var softDeleteField1 *schema.Field
	for _, field := range sch1.Fields {
		if reflect.TypeOf(field.FieldType) == reflect.TypeFor[gorm.DeletedAt]() {
			softDeleteField1 = field
			break
		}
		if (field.Name == "DeletedAt" || field.DBName == "deleted_at") &&
			(field.DataType == schema.Time || field.GORMDataType == schema.Time) {
			softDeleteField1 = field
			break
		}
	}
	if softDeleteField1 == nil {
		t.Errorf("Expected to detect soft delete field in Model1, but got nil")
	} else if softDeleteField1.Name != "DeletedAt" {
		t.Errorf("Expected soft delete field name to be 'DeletedAt', but got '%s'", softDeleteField1.Name)
	}

	// 测试场景 2: 普通 time.Time 字段名为 DeletedAt
	type Model2 struct {
		ID        uint `gorm:"primaryKey"`
		DeletedAt time.Time
	}
	sch2 := parseTestSchema(t, &Model2{})
	
	// 验证能正确检测到软删除字段
	var softDeleteField2 *schema.Field
	for _, field := range sch2.Fields {
		if reflect.TypeOf(field.FieldType) == reflect.TypeFor[gorm.DeletedAt]() {
			softDeleteField2 = field
			break
		}
		if (field.Name == "DeletedAt" || field.DBName == "deleted_at") &&
			(field.DataType == schema.Time || field.GORMDataType == schema.Time) {
			softDeleteField2 = field
			break
		}
	}
	if softDeleteField2 == nil {
		t.Errorf("Expected to detect soft delete field in Model2, but got nil")
	} else if softDeleteField2.Name != "DeletedAt" {
		t.Errorf("Expected soft delete field name to be 'DeletedAt', but got '%s'", softDeleteField2.Name)
	}

	// 测试场景 3: 普通 time.Time 字段名不是 DeletedAt
	type Model3 struct {
		ID        uint `gorm:"primaryKey"`
		CreatedAt time.Time
	}
	sch3 := parseTestSchema(t, &Model3{})
	
	// 验证不会误判为软删除字段
	var softDeleteField3 *schema.Field
	for _, field := range sch3.Fields {
		if reflect.TypeOf(field.FieldType) == reflect.TypeFor[gorm.DeletedAt]() {
			softDeleteField3 = field
			break
		}
		if (field.Name == "DeletedAt" || field.DBName == "deleted_at") &&
			(field.DataType == schema.Time || field.GORMDataType == schema.Time) {
			softDeleteField3 = field
			break
		}
	}
	if softDeleteField3 != nil {
		t.Errorf("Expected no soft delete field in Model3, but got field '%s'", softDeleteField3.Name)
	}
}

// ---- noopFinisher ----

func TestNoopFinisher(t *testing.T) {
	// noopFinisher 不应 panic 或修改 db
	db := &gorm.DB{}
	noopFinisher(db)
	// 无副作用即通过
}

// ---- outDest 补充测试 ----

func TestOutDestExtractsPointer(t *testing.T) {
	outVal := go_ora.Out{Dest: new(string)}
	vars := []any{42, outVal, "skip"}

	got := outDest(vars, 1)
	if _, ok := got.(*string); !ok {
		t.Errorf("outDest() returned %T, want *string", got)
	}
}

// ---- columnNameOf ----

func TestColumnNameOf(t *testing.T) {
	tests := []struct {
		name string
		col  any
		want string
	}{
		{"clause.Column", clause.Column{Name: "age"}, "age"},
		{"string", "name", "name"},
		{"unsupported type", 123, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := columnNameOf(tt.col)
			if got != tt.want {
				t.Errorf("columnNameOf(%v) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}

// ---- ensureWriteTx ----

func TestEnsureWriteTxDefaultPool(t *testing.T) {
	// 未知池类型：应返回原池 + noopFinisher + nil error
	type unknownPool struct{ gorm.ConnPool }
	pool := &unknownPool{}
	lg := logger.Default.LogMode(logger.Silent)

	result, finish, err := ensureWriteTx(context.Background(), pool, lg)
	if err != nil {
		t.Fatalf("ensureWriteTx() error = %v", err)
	}
	if result != pool {
		t.Error("ensureWriteTx() should return original pool for unknown type")
	}
	if finish == nil {
		t.Error("ensureWriteTx() should return non-nil finisher")
	}
	// 调用 finish 不应 panic
	finish(&gorm.DB{})
}

// ---- buildOracleDefault 补充 ----

func TestBuildOracleDefaultDateWithSingleQuote(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "2006-01-02", nil)
	want := "DEFAULT TO_DATE('2006-01-02', 'YYYY-MM-DD')"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

func TestBuildOracleDefaultTimestampWithSingleQuote(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "2006-01-02 15:04:05", nil)
	want := "DEFAULT TO_DATE('2006-01-02 15:04:05', 'YYYY-MM-DD HH24:MI:SS')"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

func TestBuildOracleDefaultPlainString(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "hello", nil)
	want := "DEFAULT 'hello'"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

func TestBuildOracleDefaultStringWithSingleQuote(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "it's", nil)
	want := "DEFAULT 'it''s'"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

func TestBuildOracleDefaultSequence12c(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "SEQ_MY.NEXTVAL", nil)
	want := "DEFAULT SEQ_MY.NEXTVAL"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

func TestBuildOracleDefaultSequence11g(t *testing.T) {
	got := buildOracleDefault("11.2.0.4.0", "SEQ_MY.NEXTVAL", nil)
	if got != "" {
		t.Errorf("buildOracleDefault() = %q, want empty string for 11g", got)
	}
}

func TestBuildOracleDefaultSequenceWithBrackets(t *testing.T) {
	got := buildOracleDefault("12.1.0.2.0", "(SEQ_MY.NEXTVAL)", nil)
	want := "DEFAULT SEQ_MY.NEXTVAL"
	if got != want {
		t.Errorf("buildOracleDefault() = %q, want %q", got, want)
	}
}

// ---- outParamSize 补充 ----

func TestOutParamSizeText(t *testing.T) {
	f := &schema.Field{DataType: "text"}
	if got := outParamSize(f); got != 4000 {
		t.Errorf("outParamSize(text) = %d, want 4000", got)
	}
}

func TestOutParamSizeJSON(t *testing.T) {
	f := &schema.Field{DataType: "json"}
	if got := outParamSize(f); got != 4000 {
		t.Errorf("outParamSize(json) = %d, want 4000", got)
	}
}

func TestOutParamSizeInt(t *testing.T) {
	f := &schema.Field{DataType: schema.Int}
	if got := outParamSize(f); got != 0 {
		t.Errorf("outParamSize(int) = %d, want 0", got)
	}
}

func TestOutParamSizeStringWithSize(t *testing.T) {
	f := &schema.Field{DataType: schema.String, Size: 100}
	if got := outParamSize(f); got != 100 {
		t.Errorf("outParamSize(string:100) = %d, want 100", got)
	}
}

func TestOutParamSizeStringNoSize(t *testing.T) {
	f := &schema.Field{DataType: schema.String}
	if got := outParamSize(f); got != 4000 {
		t.Errorf("outParamSize(string) = %d, want 4000", got)
	}
}

func TestOutParamSizeBytesWithSize(t *testing.T) {
	f := &schema.Field{DataType: schema.Bytes, Size: 512}
	if got := outParamSize(f); got != 512 {
		t.Errorf("outParamSize(bytes:512) = %d, want 512", got)
	}
}

func TestOutParamSizeBytesNoSize(t *testing.T) {
	f := &schema.Field{DataType: schema.Bytes}
	if got := outParamSize(f); got != 4000 {
		t.Errorf("outParamSize(bytes) = %d, want 4000", got)
	}
}

// ---- bareType 补充测试 ----

func TestBareTypeCoversAllBasicKinds(t *testing.T) {
	tests := []struct {
		name string
		kind reflect.Kind
		want reflect.Kind
	}{
		{"int", reflect.Int, reflect.Int64},
		{"int8", reflect.Int8, reflect.Int64},
		{"int16", reflect.Int16, reflect.Int64},
		{"int32", reflect.Int32, reflect.Int64},
		{"int64", reflect.Int64, reflect.Int64},
		{"uint", reflect.Uint, reflect.Uint64},
		{"uint8", reflect.Uint8, reflect.Uint64},
		{"uint16", reflect.Uint16, reflect.Uint64},
		{"uint32", reflect.Uint32, reflect.Uint64},
		{"uint64", reflect.Uint64, reflect.Uint64},
		{"float32", reflect.Float32, reflect.Float64},
		{"float64", reflect.Float64, reflect.Float64},
		{"bool", reflect.Bool, reflect.Bool},
		{"string", reflect.String, reflect.String},
		{"struct", reflect.Struct, reflect.Struct},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := reflect.TypeFor[any]()
			// 创建指定 kind 的类型
			switch tt.kind {
			case reflect.Int:
				in = reflect.TypeFor[int]()
			case reflect.Int8:
				in = reflect.TypeFor[int8]()
			case reflect.Int16:
				in = reflect.TypeFor[int16]()
			case reflect.Int32:
				in = reflect.TypeFor[int32]()
			case reflect.Int64:
				in = reflect.TypeFor[int64]()
			case reflect.Uint:
				in = reflect.TypeFor[uint]()
			case reflect.Uint8:
				in = reflect.TypeFor[uint8]()
			case reflect.Uint16:
				in = reflect.TypeFor[uint16]()
			case reflect.Uint32:
				in = reflect.TypeFor[uint32]()
			case reflect.Uint64:
				in = reflect.TypeFor[uint64]()
			case reflect.Float32:
				in = reflect.TypeFor[float32]()
			case reflect.Float64:
				in = reflect.TypeFor[float64]()
			case reflect.Bool:
				in = reflect.TypeFor[bool]()
			case reflect.String:
				in = reflect.TypeFor[string]()
			case reflect.Struct:
				in = reflect.TypeFor[time.Time]()
			}
			got := bareType(in)
			if got.Kind() != tt.want {
				t.Errorf("bareType(%v).Kind() = %v, want %v", in, got.Kind(), tt.want)
			}
		})
	}
}

// ---- isSoftDeleteCondition 补充 ----

func TestIsSoftDeleteCondition(t *testing.T) {
	sch := parseTestSchema(t, &softDeleteModel{})

	tests := []struct {
		name      string
		condition clause.Expression
		want      bool
	}{
		{"Eq on deleted_at", clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil}, true},
		{"IN on deleted_at", clause.IN{Column: clause.Column{Name: "deleted_at"}, Values: []any{nil}}, true},
		{"Neq on deleted_at", clause.Neq{Column: clause.Column{Name: "deleted_at"}, Value: nil}, true},
		{"Eq on other column", clause.Eq{Column: "name", Value: "x"}, false},
		{"unsupported type", clause.Expr{SQL: "1=1"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSoftDeleteCondition(tt.condition, sch)
			if got != tt.want {
				t.Errorf("isSoftDeleteCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- addPrimaryKeyWhere ----

func TestAddPrimaryKeyWhereNilInputs(t *testing.T) {
	if got := addPrimaryKeyWhere(nil, nil); got != 0 {
		t.Errorf("addPrimaryKeyWhere(nil, nil) = %d, want 0", got)
	}
}

// ---- convertValue 补充 ----

func TestConvertValueInt(t *testing.T) {
	got := convertValue(42, nil)
	if got != 42 {
		t.Errorf("convertValue(42) = %v, want 42", got)
	}
}

func TestConvertValueFloat(t *testing.T) {
	got := convertValue(3.14, nil)
	if got != 3.14 {
		t.Errorf("convertValue(3.14) = %v, want 3.14", got)
	}
}

// ---- convertFromOracleToField 补充 default 分支 ----

func TestConvertFromOracleToFieldDefault(t *testing.T) {
	got := convertFromOracleToField([]byte{1, 2, 3}, nil)
	if _, ok := got.([]byte); !ok {
		t.Errorf("convertFromOracleToField([]byte) returned %T, want []byte", got)
	}
}
