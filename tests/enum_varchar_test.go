package tests

import (
	"testing"
	"time"
)

// ===== 用户真实业务枚举形态：int 底层 + varchar(1) 列 + String() 方法 =====
//
// 该形态对应 enum.MerchantStatus / enum.MerchantIssueStatus：
//   - 枚举底层为 int，可带 String()（fmt.Stringer）；
//   - 数据库列经 gorm:"type:varchar(1)" 显式指定为 VARCHAR；
//   - 要求 Create / Save(全字段 UPDATE) / Update / Where 全链路不报错，
//     Insert/Update 后 Select 值一致（与 String() 展示名无关）。

// MerchantIssueStatus 商户合作状态：底层 int 的枚举
type EnumIssueStatus int

const (
	EnumIssueStatusCreated      EnumIssueStatus = 0
	EnumIssueStatusExistence    EnumIssueStatus = 1
	EnumIssueStatusSuspend      EnumIssueStatus = 2
	EnumIssueStatusDeregistered EnumIssueStatus = 3
)

// MerchantStatus 商户经营状态：底层 int 的枚举，带 String() 方法
type EnumMerchantStatus int

const (
	EnumMerchantStatusCreated      EnumMerchantStatus = 0
	EnumMerchantStatusExistence    EnumMerchantStatus = 1
	EnumMerchantStatusDeregistered EnumMerchantStatus = 3
)

func (s EnumMerchantStatus) String() string {
	switch s {
	case EnumMerchantStatusCreated:
		return "创建或待审核"
	case EnumMerchantStatusExistence:
		return "正常"
	case EnumMerchantStatusDeregistered:
		return "注销"
	default:
		return "未知"
	}
}

// EnumMerchant 真实业务形态：枚举字段映射到 varchar(1) 列
type EnumMerchant struct {
	ID             uint               `gorm:"column:id;primaryKey;autoIncrement"`
	MerchantStatus EnumMerchantStatus `gorm:"type:varchar(1)"` // 经营状态
	IssueStatus    EnumIssueStatus    `gorm:"type:varchar(1)"` // 合作状态
	CreatedAt      time.Time
}

func (EnumMerchant) TableName() string { return "TEST_ENUM_MERCHANTS" }

// TestEnumVarcharColumn 复现用户场景：int 底层枚举写入 varchar(1) 列
func TestEnumVarcharColumn(t *testing.T) {
	if err := DB.AutoMigrate(&EnumMerchant{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "TEST_ENUM_MERCHANTS")

	t.Run("Create含枚举字段", func(t *testing.T) {
		m := EnumMerchant{
			MerchantStatus: EnumMerchantStatusExistence,
			IssueStatus:    EnumIssueStatusSuspend,
			CreatedAt:      time.Now(),
		}
		if err := DB.Create(&m).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
		if m.ID == 0 {
			t.Fatal("Create 后主键未回填")
		}

		var got EnumMerchant
		if err := DB.First(&got, m.ID).Error; err != nil {
			t.Fatalf("First 回读失败: %v", err)
		}
		if got.MerchantStatus != EnumMerchantStatusExistence {
			t.Errorf("Create 后 MerchantStatus 不一致：期望 %d，实际 %d", int(EnumMerchantStatusExistence), int(got.MerchantStatus))
		}
		if got.IssueStatus != EnumIssueStatusSuspend {
			t.Errorf("Create 后 IssueStatus 不一致：期望 %d，实际 %d", int(EnumIssueStatusSuspend), int(got.IssueStatus))
		}
	})

	t.Run("Save全字段UPDATE", func(t *testing.T) {
		m := EnumMerchant{
			MerchantStatus: EnumMerchantStatusCreated,
			IssueStatus:    EnumIssueStatusCreated,
			CreatedAt:      time.Now(),
		}
		if err := DB.Create(&m).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}

		m.MerchantStatus = EnumMerchantStatusDeregistered
		m.IssueStatus = EnumIssueStatusDeregistered
		if err := DB.Save(&m).Error; err != nil {
			t.Fatalf("Save(全字段UPDATE) 失败: %v", err)
		}

		var got EnumMerchant
		if err := DB.First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.MerchantStatus != EnumMerchantStatusDeregistered {
			t.Errorf("Save 后 MerchantStatus 不一致：期望 %d，实际 %d", int(EnumMerchantStatusDeregistered), int(got.MerchantStatus))
		}
		if got.IssueStatus != EnumIssueStatusDeregistered {
			t.Errorf("Save 后 IssueStatus 不一致：期望 %d，实际 %d", int(EnumIssueStatusDeregistered), int(got.IssueStatus))
		}
	})

	t.Run("Update单列枚举值", func(t *testing.T) {
		m := EnumMerchant{
			MerchantStatus: EnumMerchantStatusExistence,
			IssueStatus:    EnumIssueStatusExistence,
			CreatedAt:      time.Now(),
		}
		if err := DB.Create(&m).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}

		if err := DB.Model(&EnumMerchant{}).Where("id = ?", m.ID).
			Update("merchant_status", EnumMerchantStatusDeregistered).Error; err != nil {
			t.Fatalf("Update 失败: %v", err)
		}

		var got EnumMerchant
		if err := DB.First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.MerchantStatus != EnumMerchantStatusDeregistered {
			t.Errorf("Update 后不一致：期望 %d，实际 %d", int(EnumMerchantStatusDeregistered), int(got.MerchantStatus))
		}
	})

	t.Run("Where按枚举值查询", func(t *testing.T) {
		if err := DB.Create(&EnumMerchant{
			MerchantStatus: EnumMerchantStatusExistence,
			IssueStatus:    EnumIssueStatusExistence,
			CreatedAt:      time.Now(),
		}).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}

		var got EnumMerchant
		if err := DB.Where("merchant_status = ?", EnumMerchantStatusExistence).First(&got).Error; err != nil {
			t.Fatalf("Where 查询失败: %v", err)
		}
		if got.MerchantStatus != EnumMerchantStatusExistence {
			t.Errorf("Where 查询结果不一致：%d", int(got.MerchantStatus))
		}
	})
}

// ===== string 底层枚举（带 String()）验证 =====

// EnumCode string 底层的枚举，带 String() 方法（fmt.Stringer）
type EnumCode string

const (
	EnumCodePending EnumCode = "P"
	EnumCodeActive  EnumCode = "A"
	EnumCodeClosed  EnumCode = "C"
)

func (c EnumCode) String() string {
	switch c {
	case EnumCodePending:
		return "待处理"
	case EnumCodeActive:
		return "生效中"
	case EnumCodeClosed:
		return "已关闭"
	default:
		return "未知"
	}
}

// EnumStringMerchant string 底层枚举字段映射到 varchar 列
type EnumStringMerchant struct {
	ID   uint     `gorm:"column:id;primaryKey;autoIncrement"`
	Code EnumCode `gorm:"type:varchar(2)"`
}

func (EnumStringMerchant) TableName() string { return "TEST_ENUM_STRING_MERCHANTS" }

// TestEnumStringUnderlying 验证 string 底层枚举（含 String()）的持久化
func TestEnumStringUnderlying(t *testing.T) {
	if err := DB.AutoMigrate(&EnumStringMerchant{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "TEST_ENUM_STRING_MERCHANTS")

	m := EnumStringMerchant{Code: EnumCodeActive}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	var got EnumStringMerchant
	if err := DB.First(&got, m.ID).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Code != EnumCodeActive {
		t.Errorf("string 枚举往返不一致：期望 %q，实际 %q", string(EnumCodeActive), string(got.Code))
	}

	// 原生读取确认落库为底层字符串而非 String() 返回值
	var raw string
	if err := DB.Raw("SELECT code FROM TEST_ENUM_STRING_MERCHANTS WHERE id = ?", m.ID).Scan(&raw).Error; err != nil {
		t.Fatalf("原生读取失败: %v", err)
	}
	if raw != string(EnumCodeActive) {
		t.Errorf("落库值应为 %q（底层字符串），实际 %q", string(EnumCodeActive), raw)
	}
}
