package tests

import (
	"strconv"
	"strings"
	"testing"

	oracle "github.com/charlienet/go-oracle"
)

// TestVersionDetection 验证驱动在会话建立时自动执行的版本检测：
// 通过 DB.Config.Dialector 读取驱动缓存的主版本号（DBVer），断言其
// 非空且在合理范围（Oracle 10g~23c），并在测试日志中输出检测结果，
// 供集成测试日志追溯每个测试会话对应的 Oracle 版本。
func TestVersionDetection(t *testing.T) {
	d, ok := DB.Dialector.(*oracle.Dialector)
	if !ok {
		t.Fatalf("Dialector 类型断言失败: %T", DB.Dialector)
	}
	if d.DBVer == "" {
		t.Fatal("版本检测失败：DBVer 为空（连接时未能获取数据库版本）")
	}
	major, err := strconv.Atoi(strings.Split(d.DBVer, ".")[0])
	if err != nil {
		t.Fatalf("DBVer 解析失败 %q: %v", d.DBVer, err)
	}
	if major < 10 || major > 23 {
		t.Fatalf("版本号超出合理范围: %q（主版本 %d）", d.DBVer, major)
	}
	t.Logf("版本检测结果: DBVer=%q 主版本=%d（%s）", d.DBVer, major, versionName(major))
}

func versionName(major int) string {
	switch major {
	case 10:
		return "Oracle 10g"
	case 11:
		return "Oracle 11g"
	case 12:
		return "Oracle 12c"
	case 18:
		return "Oracle 18c"
	case 19:
		return "Oracle 19c"
	case 21:
		return "Oracle 21c"
	case 23:
		return "Oracle 23ai"
	default:
		return "未知版本"
	}
}
