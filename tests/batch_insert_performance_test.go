package tests

// 本文件为批量插入性能基准（手动基准工具）：需要真实 Oracle 数据库，运行耗时较长。
// - 无 ORACLE_DSN 环境变量时跳过；
// - 常规回归测试请使用 `go test -short` 隔离本文件（testing.Short 时跳过）；
// - 耗时数据仅供参考，不设硬性性能阈值（避免 CI 抖动误报），仅断言数据正确性
//   （批量 INSERT ALL 行数与逐行执行一致）。

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// BatchPerfModel 性能测试模型（无默认值字段）
type BatchPerfModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:100"`
}

func (BatchPerfModel) TableName() string {
	return "TEST_BATCH_PERF"
}

// BatchPerfModelWithDefault 性能测试模型（有默认值字段）
type BatchPerfModelWithDefault struct {
	ID   uint   `gorm:"primaryKey"` // 非显式 autoIncrement，进入 FieldsWithDefaultDBValue
	Name string `gorm:"size:100"`
}

func (BatchPerfModelWithDefault) TableName() string {
	return "TEST_BATCH_PERF_DEF"
}

// TestBatchInsertPerformanceComparison 性能对比测试
// 对比优化前后的批量插入性能（手动基准工具，数据仅供参考）
func TestBatchInsertPerformanceComparison(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	if os.Getenv("ORACLE_DSN") == "" {
		t.Skip("ORACLE_DSN 未设置，跳过真实库性能基准测试")
	}

	// 测试不同批量大小
	batchSizes := []int{10, 100, 500, 1000}

	for _, size := range batchSizes {
		t.Run(fmt.Sprintf("BatchSize_%d", size), func(t *testing.T) {
			// 测试无默认值场景（优化后）
			t.Run("无默认值_优化后", func(t *testing.T) {
				_ = DB.Migrator().DropTable(&BatchPerfModel{})
				if err := DB.AutoMigrate(&BatchPerfModel{}); err != nil {
					t.Fatalf("failed to migrate: %v", err)
				}
				defer func() { _ = DB.Migrator().DropTable(&BatchPerfModel{}) }()

				// 生成测试数据
				items := make([]BatchPerfModel, size)
				for i := range items {
					items[i] = BatchPerfModel{ID: uint(i + 1), Name: fmt.Sprintf("Item-%d", i)}
				}

				// 测量性能
				start := time.Now()
				result := DB.Create(&items)
				elapsed := time.Since(start)

				if result.Error != nil {
					t.Fatalf("batch insert failed: %v", result.Error)
				}

				t.Logf("Size %d: elapsed=%v, rows=%d, avg=%.2fµs/row",
					size, elapsed, result.RowsAffected,
					float64(elapsed.Microseconds())/float64(size))

				// 验证数据正确性
				var count int64
				if err := DB.Model(&BatchPerfModel{}).Count(&count).Error; err != nil {
					t.Fatalf("failed to count: %v", err)
				}
				if count != int64(size) {
					t.Errorf("expected %d rows, got %d", size, count)
				}
			})

			// 测试有默认值场景（优化前，逐行执行）
			t.Run("有默认值_逐行执行", func(t *testing.T) {
				// 清理序列
				DB.Exec("DROP SEQUENCE SEQ_BATCH_PERF_DEF")
				_ = DB.Migrator().DropTable(&BatchPerfModelWithDefault{})
				if err := DB.Exec("CREATE SEQUENCE SEQ_BATCH_PERF_DEF START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
					t.Fatalf("failed to create sequence: %v", err)
				}
				if err := DB.Exec(`CREATE TABLE TEST_BATCH_PERF_DEF (
					id NUMBER(19) NOT NULL PRIMARY KEY,
					name VARCHAR2(100)
				)`).Error; err != nil {
					t.Fatalf("failed to create table: %v", err)
				}
				if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_BATCH_PERF_DEF
					BEFORE INSERT ON TEST_BATCH_PERF_DEF
					FOR EACH ROW
					BEGIN
						IF :NEW.id IS NULL THEN
							SELECT SEQ_BATCH_PERF_DEF.NEXTVAL INTO :NEW.id FROM DUAL;
						END IF;
					END;`).Error; err != nil {
					t.Fatalf("failed to create trigger: %v", err)
				}
				defer func() {
					DB.Exec("DROP TABLE TEST_BATCH_PERF_DEF PURGE")
					DB.Exec("DROP SEQUENCE SEQ_BATCH_PERF_DEF")
				}()

				// 生成测试数据
				items := make([]BatchPerfModelWithDefault, size)
				for i := range items {
					items[i] = BatchPerfModelWithDefault{Name: fmt.Sprintf("Item-%d", i)}
				}

				// 测量性能
				start := time.Now()
				result := DB.Create(&items)
				elapsed := time.Since(start)

				if result.Error != nil {
					t.Fatalf("batch insert failed: %v", result.Error)
				}

				t.Logf("Size %d: elapsed=%v, rows=%d, avg=%.2fµs/row",
					size, elapsed, result.RowsAffected,
					float64(elapsed.Microseconds())/float64(size))

				// 验证数据正确性
				var count int64
				if err := DB.Model(&BatchPerfModelWithDefault{}).Count(&count).Error; err != nil {
					t.Fatalf("failed to count: %v", err)
				}
				if count != int64(size) {
					t.Errorf("expected %d rows, got %d", size, count)
				}
			})
		})
	}
}

// TestBatchInsertPerformanceImprovement 性能提升验证
// 对比优化前后的性能差异（手动基准工具，数据仅供参考）
func TestBatchInsertPerformanceImprovement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	if os.Getenv("ORACLE_DSN") == "" {
		t.Skip("ORACLE_DSN 未设置，跳过真实库性能基准测试")
	}

	const batchSize = 100

	// 测试无默认值场景（优化后）
	_ = DB.Migrator().DropTable(&BatchPerfModel{})
	if err := DB.AutoMigrate(&BatchPerfModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&BatchPerfModel{}) }()

	items := make([]BatchPerfModel, batchSize)
	for i := range items {
		items[i] = BatchPerfModel{ID: uint(i + 1), Name: fmt.Sprintf("Item-%d", i)}
	}

	// 多次测量取平均
	iterations := 5
	totalElapsed := time.Duration(0)
	for i := 0; i < iterations; i++ {
		// 清空表
		DB.Exec("DELETE FROM TEST_BATCH_PERF")

		start := time.Now()
		result := DB.Create(&items)
		elapsed := time.Since(start)

		if result.Error != nil {
			t.Fatalf("batch insert failed: %v", result.Error)
		}

		totalElapsed += elapsed
	}

	avgElapsed := totalElapsed / time.Duration(iterations)
	t.Logf("优化后平均耗时: %v (%.2fµs/row)", avgElapsed, float64(avgElapsed.Microseconds())/float64(batchSize))

	// 测试有默认值场景（逐行执行）
	DB.Exec("DROP SEQUENCE SEQ_BATCH_PERF_DEF")
	_ = DB.Migrator().DropTable(&BatchPerfModelWithDefault{})
	if err := DB.Exec("CREATE SEQUENCE SEQ_BATCH_PERF_DEF START WITH 100 INCREMENT BY 1 NOCACHE").Error; err != nil {
		t.Fatalf("failed to create sequence: %v", err)
	}
	if err := DB.Exec(`CREATE TABLE TEST_BATCH_PERF_DEF (
		id NUMBER(19) NOT NULL PRIMARY KEY,
		name VARCHAR2(100)
	)`).Error; err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if err := DB.Exec(`CREATE OR REPLACE TRIGGER TRG_BATCH_PERF_DEF
		BEFORE INSERT ON TEST_BATCH_PERF_DEF
		FOR EACH ROW
		BEGIN
			IF :NEW.id IS NULL THEN
				SELECT SEQ_BATCH_PERF_DEF.NEXTVAL INTO :NEW.id FROM DUAL;
			END IF;
		END;`).Error; err != nil {
		t.Fatalf("failed to create trigger: %v", err)
	}
	defer func() {
		DB.Exec("DROP TABLE TEST_BATCH_PERF_DEF PURGE")
		DB.Exec("DROP SEQUENCE SEQ_BATCH_PERF_DEF")
	}()

	itemsWithDefault := make([]BatchPerfModelWithDefault, batchSize)
	for i := range itemsWithDefault {
		itemsWithDefault[i] = BatchPerfModelWithDefault{Name: fmt.Sprintf("Item-%d", i)}
	}

	totalElapsedOld := time.Duration(0)
	for i := 0; i < iterations; i++ {
		// 清空表
		DB.Exec("DELETE FROM TEST_BATCH_PERF_DEF")
		DB.Exec("DROP SEQUENCE SEQ_BATCH_PERF_DEF")
		DB.Exec("CREATE SEQUENCE SEQ_BATCH_PERF_DEF START WITH 100 INCREMENT BY 1 NOCACHE")

		start := time.Now()
		result := DB.Create(&itemsWithDefault)
		elapsed := time.Since(start)

		if result.Error != nil {
			t.Fatalf("batch insert failed: %v", result.Error)
		}

		totalElapsedOld += elapsed
	}

	avgElapsedOld := totalElapsedOld / time.Duration(iterations)
	t.Logf("逐行执行平均耗时: %v (%.2fµs/row)", avgElapsedOld, float64(avgElapsedOld.Microseconds())/float64(batchSize))

	// 计算性能提升百分比
	improvement := float64(avgElapsedOld-avgElapsed) / float64(avgElapsedOld) * 100
	// 注意：耗时对比仅作为参考值记录，不设硬性阈值（避免 CI 抖动误报）。
	// 实际性能提升取决于数据库负载、网络延迟等因素；本测试的正确性断言
	// 由各子场景的「行数一致」检查保证（见 TestBatchInsertPerformanceComparison）。
	t.Logf("性能提升: %.2f%%（参考值，不设硬阈值）", improvement)
}
