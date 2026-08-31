package oracle

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestPatchUpperDBNameKeysIdempotent(t *testing.T) {
	// 测试场景：多次调用 patchUpperDBNameKeys 应该是幂等的
	type TestModel struct {
		ID   uint   `gorm:"primaryKey;column:id"`
		Name string `gorm:"column:name"`
	}

	sch := parseTestSchema(t, &TestModel{})

	// 第一次调用
	patchUpperDBNameKeys(sch)
	field1 := sch.FieldsByDBName["NAME"]

	// 第二次调用
	patchUpperDBNameKeys(sch)
	field2 := sch.FieldsByDBName["NAME"]

	// 验证返回的是同一个字段
	if field1 != field2 {
		t.Errorf("patchUpperDBNameKeys is not idempotent")
	}
}

func TestPatchUpperDBNameKeysConcurrent(t *testing.T) {
	// 测试场景：并发调用 patchUpperDBNameKeys 不会 panic
	type TestModel struct {
		ID   uint   `gorm:"primaryKey;column:id"`
		Name string `gorm:"column:name"`
	}

	sch := parseTestSchema(t, &TestModel{})

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			patchUpperDBNameKeys(sch)
		}()
	}
	wg.Wait()

	// 验证字段存在
	if _, ok := sch.FieldsByDBName["NAME"]; !ok {
		t.Errorf("field NAME not found after concurrent patch")
	}
}

// ---- TestIsZeroValue ----

func TestIsZeroValue(t *testing.T) {
	zeroTime := time.Time{}
	now := time.Now()
	ptrNil := (*int)(nil)
	ptrVal := 42

	type zeroStruct struct {
		A int
		B string
	}

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, true},
		{"zero int", 0, true},
		{"non-zero int", 42, false},
		{"zero string", "", true},
		{"non-zero string", "abc", false},
		{"zero bool", false, true},
		{"true bool", true, false},
		{"zero float", 0.0, true},
		{"non-zero float", 3.14, false},
		{"zero time", zeroTime, true},
		{"non-zero time", now, false},
		{"nil pointer", ptrNil, true},
		{"non-nil pointer", &ptrVal, false},
		{"zero struct", zeroStruct{}, true},
		{"non-zero struct", zeroStruct{A: 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isZeroValue(tt.value); got != tt.want {
				t.Errorf("isZeroValue(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// ---- TestSetFieldValue ----

func TestSetFieldValue(t *testing.T) {
	t.Run("direct assignment", func(t *testing.T) {
		dest := 0
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, 42)
		if dest != 42 {
			t.Errorf("direct assignment got %d, want 42", dest)
		}
	})

	t.Run("type conversion", func(t *testing.T) {
		dest := 0
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, int64(7))
		if dest != 7 {
			t.Errorf("type conversion got %d, want 7", dest)
		}
	})

	t.Run("pointer field", func(t *testing.T) {
		var dest *int
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, 9)
		if dest == nil {
			t.Fatal("pointer field not set")
		}
		if *dest != 9 {
			t.Errorf("pointer field got %d, want 9", *dest)
		}
	})

	t.Run("nil value ignored", func(t *testing.T) {
		dest := 5
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, nil)
		if dest != 5 {
			t.Errorf("nil value changed dest to %d, want 5", dest)
		}
	})

	t.Run("unsettable value ignored", func(t *testing.T) {
		// 非可寻址的字段值，setFieldValue 应直接返回且不 panic
		s := "immutable"
		rv := reflect.ValueOf(s)
		setFieldValue(rv, "changed")
		if s != "immutable" {
			t.Errorf("unsettable value changed to %q", s)
		}
	})

	t.Run("pointer to pointer assign", func(t *testing.T) {
		val := 42
		src := &val
		var dest *int
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, src)
		if dest == nil || *dest != 42 {
			t.Errorf("pointer to pointer got %v, want 42", dest)
		}
	})

	t.Run("pointer kind nil value", func(t *testing.T) {
		var dest *string
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, nil)
		// dest 应保持 nil 或被显式设置为 nil
		_ = dest
	})

	t.Run("interface kind", func(t *testing.T) {
		var dest any
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, "hello")
		if dest != "hello" {
			t.Errorf("interface kind got %v, want hello", dest)
		}
	})

	t.Run("convertible types", func(t *testing.T) {
		type MyInt int
		var dest MyInt
		rv := reflect.ValueOf(&dest).Elem()
		setFieldValue(rv, int(99))
		if dest != 99 {
			t.Errorf("convertible types got %v, want 99", dest)
		}
	})
}

// ---- findSchemaFieldByStructFieldFast ----

func TestFindSchemaFieldByStructFieldFast(t *testing.T) {
	type TestModel struct {
		ID    uint   `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Email string `gorm:"column:email"`
	}

	sch := parseTestSchema(t, &TestModel{})

	nameToField := make(map[string]*schema.Field, len(sch.Fields))
	columnToField := make(map[string]*schema.Field, len(sch.Fields))
	for _, f := range sch.Fields {
		nameToField[f.Name] = f
		if f.DBName != "" {
			columnToField[strings.ToUpper(f.DBName)] = f
		}
	}

	t.Run("按 Go 字段名匹配", func(t *testing.T) {
		sf := reflect.TypeFor[TestModel]().Field(0) // ID
		got := findSchemaFieldByStructFieldFast(sch, &sf, columnToField, nameToField)
		if got == nil || got.Name != "ID" {
			t.Errorf("按 Go 字段名查找失败, got %v", got)
		}
	})

	t.Run("按 column tag 匹配", func(t *testing.T) {
		type CustomModel struct {
			UserName string `column:"name"` // 原生 column tag（非 GORM tag）
		}
		sf := reflect.TypeFor[CustomModel]().Field(0) // UserName
		got := findSchemaFieldByStructFieldFast(sch, &sf, columnToField, nameToField)
		if got == nil || got.Name != "Name" {
			t.Errorf("按 column tag 查找失败, got %v", got)
		}
	})

	t.Run("按结构体字段名大写匹配", func(t *testing.T) {
		type SimpleModel struct {
			NAME string // 无 column tag，用字段名大写匹配
		}
		sf := reflect.TypeFor[SimpleModel]().Field(0)
		got := findSchemaFieldByStructFieldFast(sch, &sf, columnToField, nameToField)
		if got == nil || got.Name != "Name" {
			t.Errorf("按结构体字段名大写查找失败, got %v", got)
		}
	})

	t.Run("遍历 schema 匹配", func(t *testing.T) {
		type MatchByName struct {
			Email string // 无 column tag，但字段名与 schema 字段名匹配
		}
		sf := reflect.TypeFor[MatchByName]().Field(0)
		got := findSchemaFieldByStructFieldFast(sch, &sf, columnToField, nameToField)
		if got == nil || got.Name != "Email" {
			t.Errorf("遍历 schema 查找失败, got %v", got)
		}
	})

	t.Run("无匹配返回 nil", func(t *testing.T) {
		type NoMatch struct {
			Unknown string
		}
		sf := reflect.TypeFor[NoMatch]().Field(0)
		got := findSchemaFieldByStructFieldFast(sch, &sf, columnToField, nameToField)
		if got != nil {
			t.Errorf("无匹配应返回 nil, got %v", got)
		}
	})

	t.Run("空映射时遍历匹配", func(t *testing.T) {
		type TraversalMatch struct {
			Name string
		}
		sf := reflect.TypeFor[TraversalMatch]().Field(0)
		got := findSchemaFieldByStructFieldFast(sch, &sf, nil, nil)
		if got == nil || got.Name != "Name" {
			t.Errorf("空映射遍历查找失败, got %v", got)
		}
	})
}

// ---- processRecord ----

func TestProcessRecord(t *testing.T) {
	type TestModel struct {
		ID   uint   `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}

	sch := parseTestSchema(t, &TestModel{})

	nameToField := make(map[string]*schema.Field, len(sch.Fields))
	columnToField := make(map[string]*schema.Field, len(sch.Fields))
	for _, f := range sch.Fields {
		nameToField[f.Name] = f
		if f.DBName != "" {
			columnToField[strings.ToUpper(f.DBName)] = f
		}
	}

	t.Run("无效值不 panic", func(t *testing.T) {
		processRecord(reflect.Value{}, sch, columnToField, nameToField)
	})

	t.Run("非结构体不 panic", func(t *testing.T) {
		s := "not a struct"
		rv := reflect.ValueOf(&s).Elem()
		processRecord(rv, sch, columnToField, nameToField)
	})

	t.Run("nil 指针创建实例", func(t *testing.T) {
		var m *TestModel
		rv := reflect.ValueOf(&m).Elem()
		processRecord(rv, sch, columnToField, nameToField)
		if m == nil {
			t.Error("nil 指针应创建实例")
		}
	})

	t.Run("已初始化的结构体", func(t *testing.T) {
		m := &TestModel{}
		processRecord(reflect.ValueOf(m).Elem(), sch, columnToField, nameToField)
	})
}

// ---- preprocessQuery ----

func TestPreprocessQueryNilStatement(t *testing.T) {
	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	// nil statement → 不 panic
	preprocessQuery(db)
}

func TestPreprocessQueryWithStatement(t *testing.T) {
	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	db.Statement = &gorm.Statement{DB: db}
	// 有 statement 但当前无需额外预处理
	preprocessQuery(db)
}

// ---- postprocessQuery ----

func TestPostprocessQueryNilDest(t *testing.T) {
	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	db.Statement = &gorm.Statement{DB: db}
	// nil dest → 不 panic
	postprocessQuery(db)
}

func TestPostprocessQueryNilSchema(t *testing.T) {
	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	db.Statement = &gorm.Statement{DB: db, Schema: nil}
	postprocessQuery(db)
}

func TestPostprocessQuerySlice(t *testing.T) {
	type TestModel struct {
		ID   uint   `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}

	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&TestModel{}); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	var dest []TestModel
	db.Statement.Dest = &dest
	postprocessQuery(db)
}

func TestPostprocessQueryStruct(t *testing.T) {
	type TestModel struct {
		ID   uint   `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}

	db, _ := gorm.Open(noopDialector{}, &gorm.Config{})
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&TestModel{}); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	dest := TestModel{}
	db.Statement.Dest = &dest
	postprocessQuery(db)
}
