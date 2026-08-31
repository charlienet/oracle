package clauses

import (
	"strings"
	"testing"

	"gorm.io/gorm/clause"
)

// allow 版本门控恒允许（模拟 12c+）
func allow() bool { return true }

// deny 版本门控恒拒绝（模拟 11g）
func deny() bool { return false }

func TestLockingName(t *testing.T) {
	l := Locking{}
	if got := l.Name(); got != "FOR" {
		t.Errorf("Locking.Name() = %q, want %q", got, "FOR")
	}
}

func TestLockingBuildForUpdate(t *testing.T) {
	l := Locking{Strength: "UPDATE"}
	if got := buildSQL(t, l); got != "FOR UPDATE" {
		t.Errorf("Locking build = %q, want %q", got, "FOR UPDATE")
	}
}

func TestLockingBuildEmptyStrength(t *testing.T) {
	l := Locking{}
	if got := buildSQL(t, l); got != "FOR UPDATE" {
		t.Errorf("Locking with empty strength = %q, want %q", got, "FOR UPDATE")
	}
}

func TestLockingBuildStrengthCaseInsensitive(t *testing.T) {
	for _, strength := range []string{"update", "Update", "UPDATE"} {
		l := Locking{Strength: strength}
		if got := buildSQL(t, l); got != "FOR UPDATE" {
			t.Errorf("Locking strength %q build = %q, want %q", strength, got, "FOR UPDATE")
		}
	}
}

func TestLockingBuildForUpdateOf(t *testing.T) {
	l := Locking{Strength: "UPDATE", Table: "t.c"}
	if got := buildSQL(t, l); got != "FOR UPDATE OF t.c" {
		t.Errorf("Locking build = %q, want %q", got, "FOR UPDATE OF t.c")
	}
}

func TestLockingBuildForUpdateOfTableOnly(t *testing.T) {
	l := Locking{Strength: "UPDATE", Table: "orders"}
	if got := buildSQL(t, l); got != "FOR UPDATE OF orders" {
		t.Errorf("Locking build = %q, want %q", got, "FOR UPDATE OF orders")
	}
}

// TestLockingBuildForShare 验证 Oracle 无 FOR SHARE：退化为 FOR UPDATE
func TestLockingBuildForShare(t *testing.T) {
	l := Locking{Strength: "SHARE"}
	if got := buildSQL(t, l); got != "FOR UPDATE" {
		t.Errorf("FOR SHARE should degrade to FOR UPDATE, got %q", got)
	}
}

// TestLockingBuildForShareCaseInsensitive 验证 FOR SHARE 大小写不敏感
func TestLockingBuildForShareCaseInsensitive(t *testing.T) {
	for _, strength := range []string{"share", "Share", "SHARE"} {
		l := Locking{Strength: strength}
		if got := buildSQL(t, l); got != "FOR UPDATE" {
			t.Errorf("Locking strength %q build = %q, want degraded FOR UPDATE", strength, got)
		}
	}
}

func TestLockingBuildUnknownStrength(t *testing.T) {
	l := Locking{Strength: "MYLOCK"}
	if got := buildSQL(t, l); got != "FOR MYLOCK" {
		t.Errorf("Locking unknown strength = %q, want %q", got, "FOR MYLOCK")
	}
}

func TestLockingBuildNowait(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "NOWAIT"}
	if got := buildSQL(t, l); got != "FOR UPDATE NOWAIT" {
		t.Errorf("Locking with NOWAIT = %q, want %q", got, "FOR UPDATE NOWAIT")
	}
}

// TestLockingBuildSkipLocked12c 验证 12c（AllowSkipLocked=true）输出 SKIP LOCKED
func TestLockingBuildSkipLocked12c(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "SKIP LOCKED", AllowSkipLocked: allow}
	if got := buildSQL(t, l); got != "FOR UPDATE SKIP LOCKED" {
		t.Errorf("12c SKIP LOCKED build = %q, want %q", got, "FOR UPDATE SKIP LOCKED")
	}
}

// TestLockingBuildSkipLocked11g 验证 11g（AllowSkipLocked=false）忽略 SKIP LOCKED
func TestLockingBuildSkipLocked11g(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "SKIP LOCKED", AllowSkipLocked: deny}
	if got := buildSQL(t, l); got != "FOR UPDATE" {
		t.Errorf("11g SKIP LOCKED should be ignored, got %q, want %q", got, "FOR UPDATE")
	}
}

// TestLockingBuildSkipLockedNoGate 验证未注入版本判定时默认允许
func TestLockingBuildSkipLockedNoGate(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
	if got := buildSQL(t, l); got != "FOR UPDATE SKIP LOCKED" {
		t.Errorf("SKIP LOCKED without gate = %q, want %q", got, "FOR UPDATE SKIP LOCKED")
	}
}

// TestLockingBuildMixedOptions12c 验证组合选项在 12c 下原样保留
func TestLockingBuildMixedOptions12c(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "NOWAIT SKIP LOCKED", AllowSkipLocked: allow}
	if got := buildSQL(t, l); got != "FOR UPDATE NOWAIT SKIP LOCKED" {
		t.Errorf("12c mixed options build = %q, want %q", got, "FOR UPDATE NOWAIT SKIP LOCKED")
	}
}

// TestLockingBuildMixedOptions11g 验证 11g 下仅移除 SKIP LOCKED、保留其余选项
func TestLockingBuildMixedOptions11g(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "NOWAIT SKIP LOCKED", AllowSkipLocked: deny}
	if got := buildSQL(t, l); got != "FOR UPDATE NOWAIT" {
		t.Errorf("11g mixed options build = %q, want %q", got, "FOR UPDATE NOWAIT")
	}
}

// TestLockingBuildOptionsCaseInsensitive 验证选项大小写不敏感（SKIP LOCKED 门控判定）
func TestLockingBuildOptionsCaseInsensitive(t *testing.T) {
	l := Locking{Strength: "update", Options: "skip locked", AllowSkipLocked: deny}
	if got := buildSQL(t, l); got != "FOR UPDATE" {
		t.Errorf("11g lowercase skip locked should be ignored, got %q", got)
	}
}

// TestLockingMergeClause 验证 MergeClause 正确保存名称与表达式
func TestLockingMergeClause(t *testing.T) {
	l := Locking{Strength: "UPDATE"}
	cc := &clause.Clause{}
	l.MergeClause(cc)
	if cc.Name != "FOR" {
		t.Errorf("MergeClause Name = %q, want %q", cc.Name, "FOR")
	}
	if _, ok := cc.Expression.(Locking); !ok {
		t.Errorf("MergeClause Expression = %T, want Locking", cc.Expression)
	}
}

// TestLockingViaClauseBuild 验证通过 clause.Clause（gorm 真实构建流程）输出。
func TestLockingViaClauseBuild(t *testing.T) {
	l := Locking{Strength: "UPDATE", Options: "NOWAIT"}
	sql := buildClauseSQL(t, "FOR", l)
	if !strings.Contains(sql, "FOR UPDATE NOWAIT") {
		t.Errorf("clause build SQL %q does not contain %q", sql, "FOR UPDATE NOWAIT")
	}
}
