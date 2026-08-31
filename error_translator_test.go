package oracle

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sijms/go-ora/v2/network"
	"gorm.io/gorm"
)

// TestOraErrorCode 验证 oraErrorCode 的结构化提取与文本兜底
func TestOraErrorCode(t *testing.T) {
	// 结构化提取：优先读取 network.OracleError.ErrCode
	if got := oraErrorCode(&network.OracleError{ErrCode: 1400}); got != 1400 {
		t.Errorf("oraErrorCode(structured) = %d, want 1400", got)
	}
	// 文本兜底：正则提取 ORA-xxxxx
	if got := oraErrorCode(errors.New("ORA-02291: parent key not found")); got != 2291 {
		t.Errorf("oraErrorCode(text) = %d, want 2291", got)
	}
	// 包裹错误：errors.As 穿透 %w 链
	if got := oraErrorCode(fmt.Errorf("wrap: %w", &network.OracleError{ErrCode: 1})); got != 1 {
		t.Errorf("oraErrorCode(wrapped) = %d, want 1", got)
	}
	// 提取不到返回 0
	if got := oraErrorCode(errors.New("connection refused")); got != 0 {
		t.Errorf("oraErrorCode(non-ora) = %d, want 0", got)
	}
	if got := oraErrorCode(errors.New("ORA-2291: 非 5 位码不匹配正则")); got != 0 {
		t.Errorf("oraErrorCode(short code) = %d, want 0（仅匹配 5 位 ORA-xxxxx）", got)
	}
	if got := oraErrorCode(nil); got != 0 {
		t.Errorf("oraErrorCode(nil) = %d, want 0", got)
	}
}

// TestTranslate 表驱动覆盖全部已映射错误码与未知码行为。
// 用例来源三种：真实 network.OracleError 构造、文本格式字符串、未知 ORA 码。
func TestTranslate(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // nil 表示期望原样返回（同一错误对象）
	}{
		// ---- 结构化：真实 network.OracleError{ErrCode: X} ----
		{"structured ORA-00001", &network.OracleError{ErrCode: 1}, gorm.ErrDuplicatedKey},
		{"structured ORA-01403", &network.OracleError{ErrCode: 1403}, gorm.ErrRecordNotFound},
		{"structured ORA-00942", &network.OracleError{ErrCode: 942}, gorm.ErrInvalidData},
		{"structured ORA-01400", &network.OracleError{ErrCode: 1400}, gorm.ErrInvalidData},
		{"structured ORA-02291", &network.OracleError{ErrCode: 2291}, gorm.ErrForeignKeyViolated},
		{"structured ORA-02292", &network.OracleError{ErrCode: 2292}, gorm.ErrForeignKeyViolated},
		{"structured ORA-02290", &network.OracleError{ErrCode: 2290}, gorm.ErrCheckConstraintViolated},
		// ---- 文本格式字符串（兜底路径） ----
		{"text ORA-00001", errors.New("ORA-00001: unique constraint (S.C) violated"), gorm.ErrDuplicatedKey},
		{"text ORA-01403", errors.New("ORA-01403: no data found"), gorm.ErrRecordNotFound},
		{"text ORA-00942", errors.New("ORA-00942: table or view does not exist"), gorm.ErrInvalidData},
		{"text ORA-01400", errors.New("ORA-01400: cannot insert NULL into (\"S\".\"T\".\"C\")"), gorm.ErrInvalidData},
		{"text ORA-02291", errors.New("ORA-02291: integrity constraint (S.FK) violated - parent key not found"), gorm.ErrForeignKeyViolated},
		{"text ORA-02292", errors.New("ORA-02292: integrity constraint (S.FK) violated - child record found"), gorm.ErrForeignKeyViolated},
		{"text ORA-02290", errors.New("ORA-02290: check constraint (S.CK) violated"), gorm.ErrCheckConstraintViolated},
		// ---- 未知 ORA 码：保持原样返回 ----
		{"unknown ORA-00060 structured", &network.OracleError{ErrCode: 60}, nil},
		{"unknown ORA-00060 text", errors.New("ORA-00060: deadlock detected while waiting for resource"), nil},
		{"unknown ORA-01722", errors.New("ORA-01722: invalid number"), nil},
		{"unknown ORA-12899", errors.New("ORA-12899: value too large for column"), nil},
		{"unknown ORA-99999", errors.New("ORA-99999: unknown"), nil},
		// ---- 非 Oracle 错误：保持原样 ----
		{"non-ora error", errors.New("connection refused"), nil},
		// ---- %w 包裹的 go-ora 错误：errors.As 穿透映射 ----
		{"wrapped structured", fmt.Errorf("exec failed: %w", &network.OracleError{ErrCode: 2290}), gorm.ErrCheckConstraintViolated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Dialector{}
			got := d.Translate(tt.err)
			if tt.want == nil {
				// 原样返回：必须与入参是同一错误对象（不包裹、不替换）
				if got != tt.err {
					t.Errorf("Translate(%v) = %v, want original error unchanged", tt.err, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Errorf("Translate(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestTranslateNil 验证 nil 输入返回 nil
func TestTranslateNil(t *testing.T) {
	d := &Dialector{}
	if got := d.Translate(nil); got != nil {
		t.Errorf("Translate(nil) = %v, want nil", got)
	}
}
