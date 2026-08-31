package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestSerializer_JSON 验证 JSON serializer 在 VARCHAR2 列上的行为
func TestSerializer_JSON(t *testing.T) {
	type Config struct {
		ID   uint
		Data map[string]any `gorm:"serializer:json;size:4000"`
	}

	if err := DB.AutoMigrate(&Config{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "CONFIGS")

	// 1. 创建记录
	input := Config{
		Data: map[string]any{
			"key":   "value",
			"count": 42,
		},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got Config
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言 Data 正确反序列化
	if got.Data == nil {
		t.Fatal("期望 Data 非空，实际为 nil")
	}
	if got.Data["key"] != "value" {
		t.Errorf("Data[\"key\"] 期望 \"value\", 实际 %v", got.Data["key"])
	}
	// JSON 反序列化数字时默认为 float64
	if count, ok := got.Data["count"].(float64); !ok || count != 42 {
		t.Errorf("Data[\"count\"] 期望 42 (float64), 实际 %v (%T)", got.Data["count"], got.Data["count"])
	}
}

// TestSerializer_JSON_CLOB 验证 JSON serializer 在 CLOB 列上的行为
func TestSerializer_JSON_CLOB(t *testing.T) {
	type LargeConfig struct {
		ID   uint
		Data map[string]any `gorm:"serializer:json;type:clob"`
	}

	if err := DB.AutoMigrate(&LargeConfig{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "LARGE_CONFIGS")

	// 1. 创建记录
	input := LargeConfig{
		Data: map[string]any{
			"large_key": "large_value",
			"nested": map[string]any{
				"inner": "data",
			},
		},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got LargeConfig
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言 Data 正确反序列化
	if got.Data == nil {
		t.Fatal("期望 Data 非空，实际为 nil")
	}
	if got.Data["large_key"] != "large_value" {
		t.Errorf("Data[\"large_key\"] 期望 \"large_value\", 实际 %v", got.Data["large_key"])
	}
	nested, ok := got.Data["nested"].(map[string]any)
	if !ok {
		t.Fatalf("Data[\"nested\"] 期望 map, 实际 %T", got.Data["nested"])
	}
	if nested["inner"] != "data" {
		t.Errorf("nested[\"inner\"] 期望 \"data\", 实际 %v", nested["inner"])
	}
}

// TestSerializer_JSON_Struct 验证 JSON serializer 在结构体字段上的行为
func TestSerializer_JSON_Struct(t *testing.T) {
	type Address struct {
		City    string `json:"city"`
		Country string `json:"country"`
	}

	type UserWithAddress struct {
		ID      uint
		Name    string  `gorm:"size:100"`
		Address Address `gorm:"serializer:json;size:1000"`
	}

	if err := DB.AutoMigrate(&UserWithAddress{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "USER_WITH_ADDRESSES")

	// 1. 创建记录
	input := UserWithAddress{
		Name: "测试用户",
		Address: Address{
			City:    "北京",
			Country: "中国",
		},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got UserWithAddress
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言 Address 正确反序列化
	if got.Address.City != "北京" {
		t.Errorf("Address.City 期望 \"北京\", 实际 %q", got.Address.City)
	}
	if got.Address.Country != "中国" {
		t.Errorf("Address.Country 期望 \"中国\", 实际 %q", got.Address.Country)
	}
}

// TestSerializer_GOB 验证 GOB serializer 的行为
// 测试在 VARCHAR2 中存储 GOB 数据，驱动层会自动将 string 转换为 []byte
func TestSerializer_GOB(t *testing.T) {
	type GobConfig struct {
		ID   uint
		Data map[string]int `gorm:"serializer:gob;size:4000"`
	}

	if err := DB.AutoMigrate(&GobConfig{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "GOB_CONFIGS")

	// 1. 创建记录
	input := GobConfig{
		Data: map[string]int{
			"one":   1,
			"two":   2,
			"three": 3,
		},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got GobConfig
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言 Data 正确反序列化
	if got.Data == nil {
		t.Fatal("期望 Data 非空，实际为 nil")
	}
	if got.Data["one"] != 1 {
		t.Errorf("Data[\"one\"] 期望 1, 实际 %d", got.Data["one"])
	}
	if got.Data["two"] != 2 {
		t.Errorf("Data[\"two\"] 期望 2, 实际 %d", got.Data["two"])
	}
	if got.Data["three"] != 3 {
		t.Errorf("Data[\"three\"] 期望 3, 实际 %d", got.Data["three"])
	}
}

// TestSerializer_UnixTime 验证 unixtime serializer 的真实往返
// gorm 的 UnixSecondSerializer 语义：int64 字段（Unix 秒）序列化为 time.Time 存储
// （列型 TIMESTAMP WITH TIME ZONE），读取时经 sql.NullTime 转回 int64。
// 此前 LIMITATIONS.md 记录的「不支持」实为类型映射错误（int64 被映射为 NUMBER，
// 写入报 ORA-00932、读取 int64→time.Time 失败）；已修复：unixtime 字段映射
// TIMESTAMP 系列，本测试验证端到端往返，同时保留 int64 直存（无序列化器）用例。
func TestSerializer_UnixTime(t *testing.T) {
	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	// 场景 1：unixtime serializer 真实往返
	t.Run("unixtime serializer 往返", func(t *testing.T) {
		type Event struct {
			ID        uint
			Name      string `gorm:"size:100"`
			Timestamp int64  `gorm:"serializer:unixtime"`
		}

		if err := DB.AutoMigrate(&Event{}); err != nil {
			t.Fatalf("AutoMigrate 失败: %v", err)
		}
		clearTable(t, "EVENTS")

		// 创建记录（已知时间的 Unix 时间戳）
		input := Event{
			Name:      "测试事件",
			Timestamp: expectedTime.Unix(),
		}
		if err := DB.Create(&input).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
		if input.ID == 0 {
			t.Fatal("期望 ID 被回填，实际为 0")
		}

		// 查询记录
		var got Event
		if err := DB.First(&got, input.ID).Error; err != nil {
			t.Fatalf("First 失败: %v", err)
		}

		// 断言 Unix 秒一致（serializer 往返成功）
		if got.Timestamp != expectedTime.Unix() {
			t.Errorf("Timestamp 期望 %d, 实际 %d", expectedTime.Unix(), got.Timestamp)
		}

		// 断言列类型为 TIMESTAMP 系列（非 NUMBER）
		var found bool
		columnTypes, err := DB.Migrator().ColumnTypes(&Event{})
		if err != nil {
			t.Fatalf("ColumnTypes 失败: %v", err)
		}
		for _, ct := range columnTypes {
			if ct.Name() == "TIMESTAMP" {
				found = true
				if !strings.Contains(strings.ToUpper(ct.DatabaseTypeName()), "TIMESTAMP") {
					t.Errorf("期望列类型包含 TIMESTAMP, 实际 %q", ct.DatabaseTypeName())
				}
			}
		}
		if !found {
			t.Error("未找到 TIMESTAMP 列（unixtime 字段应映射为 TIMESTAMP 系列）")
		}
	})

	// 场景 2：int64 直存（不使用序列化器），保留原有覆盖
	t.Run("int64 直存", func(t *testing.T) {
		type EventPlain struct {
			ID        uint
			Name      string `gorm:"size:100"`
			Timestamp int64  // 直接存储 Unix 时间戳，不使用序列化器
		}

		if err := DB.AutoMigrate(&EventPlain{}); err != nil {
			t.Fatalf("AutoMigrate 失败: %v", err)
		}
		clearTable(t, "EVENT_PLAINS")

		// 创建记录（使用已知时间的 Unix 时间戳）
		input := EventPlain{
			Name:      "测试事件",
			Timestamp: expectedTime.Unix(),
		}
		if err := DB.Create(&input).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
		if input.ID == 0 {
			t.Fatal("期望 ID 被回填，实际为 0")
		}

		// 查询记录
		var got EventPlain
		if err := DB.First(&got, input.ID).Error; err != nil {
			t.Fatalf("First 失败: %v", err)
		}

		// 断言 Timestamp 正确
		if got.Timestamp != expectedTime.Unix() {
			t.Errorf("Timestamp 期望 %d, 实际 %d", expectedTime.Unix(), got.Timestamp)
		}
	})
}

// TestSerializer_JSON_Update 验证 JSON serializer 在更新时的行为
func TestSerializer_JSON_Update(t *testing.T) {
	type UpdateConfig struct {
		ID   uint
		Data map[string]string `gorm:"serializer:json;size:1000"`
	}

	if err := DB.AutoMigrate(&UpdateConfig{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "UPDATE_CONFIGS")

	// 1. 创建记录
	input := UpdateConfig{
		Data: map[string]string{
			"initial": "value",
		},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}

	// 2. 更新记录
	input.Data["new"] = "updated"
	if err := DB.Save(&input).Error; err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 3. 查询并验证
	var got UpdateConfig
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}
	if got.Data["initial"] != "value" {
		t.Errorf("Data[\"initial\"] 期望 \"value\", 实际 %v", got.Data["initial"])
	}
	if got.Data["new"] != "updated" {
		t.Errorf("Data[\"new\"] 期望 \"updated\", 实际 %v", got.Data["new"])
	}
}

// TestSerializer_JSON_Slice 验证 JSON serializer 在切片字段上的行为
func TestSerializer_JSON_Slice(t *testing.T) {
	type SliceConfig struct {
		ID   uint
		Tags []string `gorm:"serializer:json;size:1000"`
	}

	if err := DB.AutoMigrate(&SliceConfig{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "SLICE_CONFIGS")

	// 1. 创建记录
	input := SliceConfig{
		Tags: []string{"tag1", "tag2", "tag3"},
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got SliceConfig
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言 Tags 正确反序列化
	if len(got.Tags) != 3 {
		t.Fatalf("期望 Tags 长度为 3, 实际 %d", len(got.Tags))
	}
	if got.Tags[0] != "tag1" {
		t.Errorf("Tags[0] 期望 \"tag1\", 实际 %q", got.Tags[0])
	}
	if got.Tags[1] != "tag2" {
		t.Errorf("Tags[1] 期望 \"tag2\", 实际 %q", got.Tags[1])
	}
	if got.Tags[2] != "tag3" {
		t.Errorf("Tags[2] 期望 \"tag3\", 实际 %q", got.Tags[2])
	}
}

// TestSerializer_JSON_Empty 验证 JSON serializer 对空值的处理
func TestSerializer_JSON_Empty(t *testing.T) {
	type EmptyConfig struct {
		ID   uint
		Data map[string]string `gorm:"serializer:json;size:1000"`
	}

	if err := DB.AutoMigrate(&EmptyConfig{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "EMPTY_CONFIGS")

	// 1. 创建空记录
	input := EmptyConfig{
		Data: nil,
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got EmptyConfig
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言空值处理正确
	// GORM 的 json serializer 应该将 nil map 反序列化为空 map 或 nil
	// 具体行为取决于实现
	if len(got.Data) > 0 {
		t.Errorf("期望 Data 为空, 实际 %v", got.Data)
	}
}

// TestSerializer_JSON_LargeValue 验证大 JSON 值的处理
func TestSerializer_JSON_LargeValue(t *testing.T) {
	type LargeData struct {
		ID   uint
		Data string `gorm:"serializer:json;type:clob"`
	}

	if err := DB.AutoMigrate(&LargeData{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	clearTable(t, "LARGE_DATA")

	// 1. 创建大 JSON 记录（超过 4000 字节）
	largeObj := make(map[string]string)
	for i := 0; i < 500; i++ {
		largeObj[fmt.Sprintf("key_%d", i)] = fmt.Sprintf("value_%d", i)
	}
	jsonBytes, err := json.Marshal(largeObj)
	if err != nil {
		t.Fatalf("序列化大对象失败: %v", err)
	}

	input := LargeData{
		Data: string(jsonBytes),
	}
	if err := DB.Create(&input).Error; err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if input.ID == 0 {
		t.Fatal("期望 ID 被回填，实际为 0")
	}

	// 2. 查询记录
	var got LargeData
	if err := DB.First(&got, input.ID).Error; err != nil {
		t.Fatalf("First 失败: %v", err)
	}

	// 3. 断言大值正确存储和读取
	if len(got.Data) != len(jsonBytes) {
		t.Errorf("期望 Data 长度 %d, 实际 %d", len(jsonBytes), len(got.Data))
	}
}
