package utils

import (
	"reflect"
	"strconv"
	"testing"

	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"
)

// TestMapString 测试 MapString：将切片元素映射为字符串
func TestMapString(t *testing.T) {
	toString := func(v int) string { return strconv.Itoa(v) }

	tests := []struct {
		name  string
		slice []int
		want  []string
	}{
		{"正常输入", []int{1, 2, 3}, []string{"1", "2", "3"}},
		{"空切片", []int{}, []string{}},
		{"nil输入", nil, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapString(tt.slice, toString); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMapInterface 测试 MapInterface：将切片元素映射为 any
func TestMapInterface(t *testing.T) {
	toAny := func(v int) any { return v * 2 }

	tests := []struct {
		name  string
		slice []int
		want  []any
	}{
		{"正常输入", []int{1, 2, 3}, []any{2, 4, 6}},
		{"空切片", []int{}, []any{}},
		{"nil输入", nil, []any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapInterface(tt.slice, toAny); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMapClauseColumn 测试 MapClauseColumn：将 clause.Column 切片映射为新的 clause.Column
func TestMapClauseColumn(t *testing.T) {
	prefix := func(c clause.Column) clause.Column {
		return clause.Column{Table: c.Table, Name: "U_" + c.Name}
	}

	tests := []struct {
		name  string
		slice []clause.Column
		want  []clause.Column
	}{
		{
			"正常输入",
			[]clause.Column{{Table: "users", Name: "id"}, {Table: "users", Name: "name"}},
			[]clause.Column{{Table: "users", Name: "U_id"}, {Table: "users", Name: "U_name"}},
		},
		{"空切片", []clause.Column{}, []clause.Column{}},
		{"nil输入", nil, []clause.Column{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapClauseColumn(tt.slice, prefix); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapClauseColumn() = %v, want %v", got, tt.want)
			}
		})
	}
}

// newTestFields 构造最小化的 schema.Field 实例列表（仅设置 Name 与 DBName）
func newTestFields() []*gormSchema.Field {
	return []*gormSchema.Field{
		{Name: "Name", DBName: "NAME"},
		{Name: "Age", DBName: "AGE"},
	}
}

// TestMapFieldToColumn 测试 MapFieldToColumn：将 schema.Field 切片映射为 clause.Column 切片
func TestMapFieldToColumn(t *testing.T) {
	toColumn := func(f *gormSchema.Field) clause.Column {
		return clause.Column{Name: f.DBName}
	}

	tests := []struct {
		name   string
		fields []*gormSchema.Field
		want   []clause.Column
	}{
		{
			"正常输入",
			newTestFields(),
			[]clause.Column{{Name: "NAME"}, {Name: "AGE"}},
		},
		{"空切片", []*gormSchema.Field{}, []clause.Column{}},
		{"nil输入", nil, []clause.Column{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapFieldToColumn(tt.fields, toColumn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapFieldToColumn() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMapFieldToExpr 测试 MapFieldToExpr：将 schema.Field 切片映射为 clause.Expression 切片
func TestMapFieldToExpr(t *testing.T) {
	toExpr := func(f *gormSchema.Field) clause.Expression {
		return clause.Eq{Column: clause.Column{Name: f.DBName}, Value: 1}
	}

	tests := []struct {
		name   string
		fields []*gormSchema.Field
		want   []clause.Expression
	}{
		{
			"正常输入",
			newTestFields(),
			[]clause.Expression{
				clause.Eq{Column: clause.Column{Name: "NAME"}, Value: 1},
				clause.Eq{Column: clause.Column{Name: "AGE"}, Value: 1},
			},
		},
		{"空切片", []*gormSchema.Field{}, []clause.Expression{}},
		{"nil输入", nil, []clause.Expression{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapFieldToExpr(tt.fields, toExpr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapFieldToExpr() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestContainsString 测试 ContainsString：判断切片是否包含指定字符串
func TestContainsString(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{"找到", []string{"a", "b", "c"}, "b", true},
		{"未找到", []string{"a", "b", "c"}, "d", false},
		{"空切片", []string{}, "a", false},
		{"nil输入", nil, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsString(tt.slice, tt.item); got != tt.want {
				t.Errorf("ContainsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestContainsSliceString 测试 ContainsSliceString：判断 inner 是否为 outer 的子集
func TestContainsSliceString(t *testing.T) {
	tests := []struct {
		name  string
		outer []string
		inner []string
		want  bool
	}{
		{"inner是outer子集", []string{"a", "b", "c"}, []string{"a", "c"}, true},
		{"inner非子集", []string{"a", "b"}, []string{"a", "d"}, false},
		{"inner为空", []string{"a", "b"}, []string{}, true},
		{"outer为空inner非空", []string{}, []string{"a"}, false},
		{"两者均为nil", nil, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsSliceString(tt.outer, tt.inner); got != tt.want {
				t.Errorf("ContainsSliceString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestContainsField 测试 ContainsField：判断 map 中是否存在指定 key
func TestContainsField(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]int
		key  string
		want bool
	}{
		{"key存在", map[string]int{"a": 1, "b": 2}, "a", true},
		{"key不存在", map[string]int{"a": 1}, "z", false},
		{"空map", map[string]int{}, "a", false},
		{"nil map", nil, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsField(tt.m, tt.key); got != tt.want {
				t.Errorf("ContainsField() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIndexOf 测试 IndexOf：查找元素在切片中的下标，未找到返回 -1
func TestIndexOf(t *testing.T) {
	tests := []struct {
		name  string
		slice []clause.Column
		item  clause.Column
		want  int
	}{
		{
			"找到",
			[]clause.Column{{Name: "id"}, {Name: "name"}},
			clause.Column{Name: "name"},
			1,
		},
		{
			"未找到",
			[]clause.Column{{Name: "id"}},
			clause.Column{Name: "xxx"},
			-1,
		},
		{"空切片", []clause.Column{}, clause.Column{Name: "id"}, -1},
		{"nil输入", nil, clause.Column{Name: "id"}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IndexOf(tt.slice, tt.item); got != tt.want {
				t.Errorf("IndexOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFilterFields 测试 FilterFields：按条件过滤 schema.Field 切片
func TestFilterFields(t *testing.T) {
	isUpperCase := func(f *gormSchema.Field) bool {
		return f.Name == "Name"
	}

	tests := []struct {
		name   string
		fields []*gormSchema.Field
		want   []*gormSchema.Field
	}{
		{"部分过滤", newTestFields(), []*gormSchema.Field{{Name: "Name", DBName: "NAME"}}},
		{
			"全部保留",
			[]*gormSchema.Field{{Name: "Name", DBName: "NAME"}},
			[]*gormSchema.Field{{Name: "Name", DBName: "NAME"}},
		},
		{"全部过滤", []*gormSchema.Field{{Name: "Age", DBName: "AGE"}}, nil},
		{"空切片", []*gormSchema.Field{}, nil},
		{"nil输入", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilterFields(tt.fields, isUpperCase); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterFields() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestForEachField 测试 ForEachField：遍历 schema.Field 切片并执行回调
func TestForEachField(t *testing.T) {
	tests := []struct {
		name   string
		fields []*gormSchema.Field
		want   []string
	}{
		{"正常输入", newTestFields(), []string{"Name", "Age"}},
		{"空切片", []*gormSchema.Field{}, nil},
		{"nil输入", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			ForEachField(tt.fields, func(f *gormSchema.Field) {
				got = append(got, f.Name)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ForEachField() 收集结果 = %v, want %v", got, tt.want)
			}
		})
	}
}
