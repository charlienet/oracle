package oracle

import (
	"sync"
	"testing"
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