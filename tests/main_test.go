package tests

import (
	"log"
	"os"
	"testing"

	"gorm.io/gorm"

	oracle "github.com/charlienet/oracle"
)

var DB *gorm.DB

func TestMain(m *testing.M) {
	// 使用提供的 DSN（可通过 ORACLE_DSN 环境变量覆盖）
	dsn := os.Getenv("ORACLE_DSN")
	if dsn == "" {
		// 注：go-ora v2.9.0 起 CONNECTION TIMEOUT 语义从"socket 读超时"变为"连接建立超时"；
		// 读超时改用 SOCKET TIMEOUT 指定。两者均设 90s 以保留原有的读超时保护语义。
		// 安全：此处为占位符，真实 DSN 请通过 ORACLE_DSN 环境变量提供，避免凭据入库。
		dsn = "oracle://user:password@host:1521/service?SSL=false&CONNECTION TIMEOUT=90&SOCKET TIMEOUT=90&LANGUAGE=SIMPLIFIED+CHINESE&TERRITORY=CHINA"
	}

	var err error
	DB, err = gorm.Open(oracle.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	log.Println("successfully connected to database")

	// 清理测试表
	cleanup()

	// 运行测试
	code := m.Run()

	// 最终清理
	cleanup()

	os.Exit(code)
}

// cleanup 删除所有测试表（忽略错误，因为表可能不存在）
func cleanup() {
	_ = DB.Migrator().DropTable(&User{}, &Product{}, &Order{}, &UserWithHook{}, &SeqDefaultViaDriverModel{}, &BigStringModel{})
	// TEST_SEQ_DEFAULT 表通过原生 SQL 创建（无 autoIncrement），DropTable 无法识别，
	// 因此用原生 SQL 清理表与序列
	DB.Exec("DROP TABLE TEST_SEQ_DEFAULT")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEFAULT")
	// 序列默认值测试的独立序列（TEST_SEQ_DEF 表可通过 DropTable 清理并级联删除触发器）
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF_CODE")
	DB.Exec("DROP SEQUENCE SEQ_TEST_SEQ_DEF")
}

// clearTable 清空指定测试表，保证测试之间的数据隔离
func clearTable(t *testing.T, table string) {
	t.Helper()
	if err := DB.Exec("DELETE FROM " + table).Error; err != nil {
		t.Fatalf("failed to clear table %s: %v", table, err)
	}
}
