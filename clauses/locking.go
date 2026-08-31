package clauses

import (
	"strings"

	"gorm.io/gorm/clause"
)

// Locking 实现 Oracle 的 SELECT ... FOR UPDATE 行锁子句。
//
// Oracle 语义说明：
//   - FOR UPDATE：对选中行加排他锁（默认行为）；
//   - FOR UPDATE OF <table>.<col>：仅锁指定表/列所在行；
//   - FOR SHARE：Oracle 无对应语法（无共享锁），保守退化为 FOR UPDATE；
//   - Options（原样输出的锁选项）：
//   - NOWAIT —— 行被占用时不等待，直接报 ORA-00054；
//   - SKIP LOCKED —— 跳过已加锁行，返回未锁行（12c 引入）。
//
// 11g 限制：SKIP LOCKED 是 Oracle 12c 才引入的语法，11g 及以下使用会报
// ORA-00933。通过 AllowSkipLocked 字段注入版本判定函数（由 oracle.go 注册
// ClauseBuilders 时注入 supportsFetchOffset(d.DBVer)）；不允许时静默忽略
// SKIP LOCKED 部分。builder 接口不暴露 logger，无法在此告警，仅以注释说明。
type Locking struct {
	// Strength 锁强度："UPDATE"（默认，大小写不敏感）或 "SHARE"（Oracle 无对应，退化为 FOR UPDATE）
	Strength string
	// Table OF 子句限定（FOR UPDATE OF <table>.<col>）；空则输出无 OF 的 FOR UPDATE
	Table string
	// Options 锁选项：NOWAIT / SKIP LOCKED 等，按空白分隔逐 token 处理
	Options string
	// AllowSkipLocked 版本门控判定：返回 true 时允许输出 SKIP LOCKED；
	// nil 视为允许（无版本限制）。
	AllowSkipLocked func() bool
}

// Name 子句名，与 gorm clause.Locking 一致（gorm 的 queryClauses 以 "FOR" 触发）。
func (locking Locking) Name() string {
	return "FOR"
}

// MergeClause 保存自身，避免 gorm 内嵌提升导致 Expression 被覆盖。
func (locking Locking) MergeClause(c *clause.Clause) {
	c.Name = locking.Name()
	c.Expression = locking
}

// Build 输出 Oracle 的 FOR UPDATE ... 子句。
func (locking Locking) Build(builder clause.Builder) {
	strength := strings.ToUpper(strings.TrimSpace(locking.Strength))
	switch strength {
	case "", "UPDATE":
		_, _ = builder.WriteString("FOR UPDATE")
	case "SHARE":
		// Oracle 无 FOR SHARE，保守退化为 FOR UPDATE（保证行级排他锁语义）
		_, _ = builder.WriteString("FOR UPDATE")
	default:
		// 未知强度：原样输出 FOR <strength>（不吞掉用户意图）
		_, _ = builder.WriteString("FOR ")
		_, _ = builder.WriteString(strength)
	}

	if table := strings.TrimSpace(locking.Table); table != "" {
		_, _ = builder.WriteString(" OF ")
		_, _ = builder.WriteString(table)
	}

	if options := strings.TrimSpace(locking.Options); options != "" {
		allowSkipLocked := locking.AllowSkipLocked == nil || locking.AllowSkipLocked()
		// 逐 token 处理：SKIP / LOCKED 成对构成 SKIP LOCKED 选项，受版本门控；
		// 其余选项（NOWAIT 等）原样输出。11g 下 SKIP LOCKED 静默移除。
		kept := make([]string, 0, 4)
		for _, token := range strings.Fields(options) {
			switch strings.ToUpper(token) {
			case "SKIP", "LOCKED":
				if allowSkipLocked {
					kept = append(kept, token)
				}
			default:
				kept = append(kept, token)
			}
		}
		if len(kept) > 0 {
			_ = builder.WriteByte(' ')
			_, _ = builder.WriteString(strings.Join(kept, " "))
		}
	}
}
