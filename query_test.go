package oracle

import (
	"reflect"
	"sync"
	"testing"
	"time"
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
	for i := 0; i < 100; i++ {
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
}