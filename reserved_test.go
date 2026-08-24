package oracle

import (
	"strings"
	"testing"
)

// TestIsReservedWord 验证常见 SQL 保留字能被正确识别为保留字。
func TestIsReservedWord(t *testing.T) {
	tests := []struct {
		name string // 用例名称
		word string // 待检测单词
		want bool   // 期望结果
	}{
		// 常见 DDL/DML 关键字
		{"SELECT", "SELECT", true},
		{"FROM", "FROM", true},
		{"WHERE", "WHERE", true},
		{"TABLE", "TABLE", true},
		{"CREATE", "CREATE", true},
		{"INSERT", "INSERT", true},
		{"UPDATE", "UPDATE", true},
		{"DELETE", "DELETE", true},
		// 逻辑运算符与条件关键字
		{"AND", "AND", true},
		{"OR", "OR", true},
		{"NOT", "NOT", true},
		{"NULL", "NULL", true},
		// 排序与分组关键字
		{"GROUP", "GROUP", true},
		{"ORDER", "ORDER", true},
		{"BY", "BY", true},
		{"HAVING", "HAVING", true},
		// 其他常见保留字
		{"INDEX", "INDEX", true},
		{"DROP", "DROP", true},
		{"ALTER", "ALTER", true},
		{"UNION", "UNION", true},
		{"VALUES", "VALUES", true},
		{"VIEW", "VIEW", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReservedWord(tt.word); got != tt.want {
				t.Errorf("IsReservedWord(%q) = %v, want %v", tt.word, got, tt.want)
			}
		})
	}
}

// TestIsReservedWordCaseSensitive 验证 IsReservedWord 对大小写敏感：
// 小写或混合大小写的写法不应被识别为保留字。
func TestIsReservedWordCaseSensitive(t *testing.T) {
	tests := []struct {
		name string // 用例名称
		word string // 待检测单词
	}{
		{"全小写 select", "select"},
		{"首字母大写 Select", "Select"},
		{"混合大小写 SeLeCt", "SeLeCt"},
		{"全小写 from", "from"},
		{"全小写 where", "where"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsReservedWord(tt.word) {
				t.Errorf("IsReservedWord(%q) = true, want false（应区分大小写）", tt.word)
			}
		})
	}
}

// TestIsReservedWordNonReserved 验证非保留字（普通标识符、空字符串等）
// 不应被识别为保留字。
func TestIsReservedWordNonReserved(t *testing.T) {
	tests := []struct {
		name string // 用例名称
		word string // 待检测单词
	}{
		{"普通标识符 FOOBAR", "FOOBAR"},
		{"下划线标识符 MY_COLUMN", "MY_COLUMN"},
		{"空字符串", ""},
		{"非保留字 JOIN", "JOIN"},
		{"非保留字 LIMIT", "LIMIT"},
		{"非保留字 OFFSET", "OFFSET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsReservedWord(tt.word) {
				t.Errorf("IsReservedWord(%q) = true, want false（该词不是保留字）", tt.word)
			}
		})
	}
}

// TestReservedWordsListNotEmpty 验证保留字列表不为空。
func TestReservedWordsListNotEmpty(t *testing.T) {
	if len(ReservedWordsList) == 0 {
		t.Error("ReservedWordsList 不应为空")
	}
}

// TestReservedWordsListAllUppercase 验证保留字列表中的每个单词均为全大写，
// 且不含空字符串。
func TestReservedWordsListAllUppercase(t *testing.T) {
	for _, word := range ReservedWordsList {
		if word == "" {
			t.Error("ReservedWordsList 中存在空字符串")
			continue
		}
		if strings.ToUpper(word) != word {
			t.Errorf("ReservedWordsList 中存在非全大写单词 %q", word)
		}
	}
}

// TestReservedWordsListNoDuplicates 验证保留字列表中不存在重复单词。
func TestReservedWordsListNoDuplicates(t *testing.T) {
	seen := make(map[string]int, len(ReservedWordsList))
	for _, word := range ReservedWordsList {
		seen[word]++
	}
	for word, count := range seen {
		if count > 1 {
			t.Errorf("ReservedWordsList 中单词 %q 出现 %d 次，存在重复", word, count)
		}
	}
}

// TestReservedWordsMapConsistency 双向验证 reservedWordsMap 与 ReservedWordsList
// 内容完全一致：列表中的词都在 map 中、map 中的 key 都在列表中、且长度一致。
func TestReservedWordsMapConsistency(t *testing.T) {
	if reservedWordsMap == nil {
		t.Fatal("reservedWordsMap 未初始化")
	}

	// 列表中的每个单词都应在 map 中存在
	for _, word := range ReservedWordsList {
		if _, ok := reservedWordsMap[word]; !ok {
			t.Errorf("ReservedWordsList 中的 %q 未出现在 reservedWordsMap 中", word)
		}
	}

	// map 中的每个 key 都应在列表中存在（防止反向不一致）
	listSet := make(map[string]struct{}, len(ReservedWordsList))
	for _, word := range ReservedWordsList {
		listSet[word] = struct{}{}
	}
	for word := range reservedWordsMap {
		if _, ok := listSet[word]; !ok {
			t.Errorf("reservedWordsMap 中的 %q 未出现在 ReservedWordsList 中", word)
		}
	}

	// map 与列表的长度应一致
	if len(reservedWordsMap) != len(ReservedWordsList) {
		t.Errorf("reservedWordsMap 长度 %d 与 ReservedWordsList 长度 %d 不一致", len(reservedWordsMap), len(ReservedWordsList))
	}
}
