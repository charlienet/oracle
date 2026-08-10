package utils

import (
	"gorm.io/gorm/clause"
	gormSchema "gorm.io/gorm/schema"
)

// 辅助函数：实现 funk.Map 功能
func MapString[T any](slice []T, fn func(T) string) []string {
	result := make([]string, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}

func MapInterface[T any](slice []T, fn func(T) interface{}) []interface{} {
	result := make([]interface{}, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}

func MapClauseColumn(slice []clause.Column, fn func(clause.Column) clause.Column) []clause.Column {
	result := make([]clause.Column, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}

func MapFieldToColumn(slice []*gormSchema.Field, fn func(*gormSchema.Field) clause.Column) []clause.Column {
	result := make([]clause.Column, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}

func MapFieldToExpr(slice []*gormSchema.Field, fn func(*gormSchema.Field) clause.Expression) []clause.Expression {
	result := make([]clause.Expression, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}

// 辅助函数：实现 funk.Contains 功能
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func ContainsSliceString(outer []string, inner []string) bool {
	for _, item := range inner {
		if !ContainsString(outer, item) {
			return false
		}
	}
	return true
}

func ContainsField(m map[string]int, key string) bool {
	_, exists := m[key]
	return exists
}

// 辅助函数：实现 funk.IndexOf 功能
func IndexOf(slice []clause.Column, item clause.Column) int {
	for i, v := range slice {
		if v == item {
			return i
		}
	}
	return -1
}

// 辅助函数：实现 funk.Filter 功能
func FilterFields(slice []*gormSchema.Field, fn func(*gormSchema.Field) bool) []*gormSchema.Field {
	var result []*gormSchema.Field
	for _, v := range slice {
		if fn(v) {
			result = append(result, v)
		}
	}
	return result
}

// 辅助函数：实现 funk.ForEach 功能
func ForEachField(slice []*gormSchema.Field, fn func(*gormSchema.Field)) {
	for _, v := range slice {
		fn(v)
	}
}