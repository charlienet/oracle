package oracle

import (
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
)

func TestGetTypeAliases(t *testing.T) {
	m := Migrator{Migrator: migrator.Migrator{}}

	tests := []struct {
		name   string
		dbType string
		want   []string
	}{
		{"number reports integer alias", "number", []string{"integer", "smallint"}},
		{"NUMBER uppercase", "NUMBER", []string{"integer", "smallint"}},
		{"Number mixed case", "Number", []string{"integer", "smallint"}},
		{"varchar2 returns nil", "varchar2", nil},
		{"float returns nil", "float", nil},
		{"clob returns nil", "clob", nil},
		{"empty string returns nil", "", nil},
		{"unknown type returns nil", "timestamp with time zone", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.GetTypeAliases(tt.dbType)
			if len(got) != len(tt.want) {
				t.Fatalf("GetTypeAliases(%q) = %v, want %v", tt.dbType, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetTypeAliases(%q)[%d] = %q, want %q", tt.dbType, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetTypeAliasesSignature(t *testing.T) {
	// 确保 GetTypeAliases 实现了 gorm.Migrator 接口中的同名方法
	var m gorm.Migrator = Migrator{Migrator: migrator.Migrator{}}
	_ = m // 编译通过即证明接口满足
}
