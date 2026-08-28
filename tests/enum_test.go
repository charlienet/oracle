package tests

import (
	"testing"
	"time"
)

// verifiedRaw 以原生 SQL 读取 VERIFIED 列的原始落库值（NUMBER(1) 编码的 "0"/"1"）。
//
// 已知限制（上游 go-ora 既有行为，非本次修复回归）：
// go-ora 对 NUMBER 列统一解码为 string（上游 parameter.go 数据读出路径
// `case NUMBER: par.oPrimValue, err = num.String()`，为避免浮点精度丢失的有意设计）；
// database/sql 的 convertAssign 仅对裸 *bool 提供 string→bool 的精确类型特判，
// named bool（MerchantBool）经 struct Scan 会报 unsupported Scan。
// 因此 named bool 的落库断言走原生读取，写路径（不触发 UDT 错误）由各子测试独立覆盖。
func verifiedRaw(t *testing.T, id uint) string {
	t.Helper()
	var v string
	if err := DB.Raw("SELECT verified FROM TEST_MERCHANTS WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("原生读取 verified 失败: %v", err)
	}
	return v
}

// TestMerchantEnumRoundTrip 端到端回归：GORM 完整对象中「底层为基本类型的自定义类型」
// （裸 enum，无 serializer / Valuer / Scanner）经 go-ora 驱动默认链路的读写一致性，
// 复现并守护 setDataType 按 Kind 回落编码修复（避免坠入 UDT 分支报错）。
func TestMerchantEnumRoundTrip(t *testing.T) {
	if err := DB.AutoMigrate(&Merchant{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_MERCHANTS")

	t.Run("Create_完整对象含enum字段", func(t *testing.T) {
		m := Merchant{
			Name:      MerchantName("商户A"),
			Status:    MerchantStatusExistence,
			Verified:  MerchantBool(true),
			CreatedAt: time.Now(),
		}

		result := DB.Create(&m)
		if result.Error != nil {
			t.Fatalf("创建完整对象失败: %v", result.Error)
		}
		if m.ID == 0 {
			t.Fatal("期望 Create 后主键 ID 被回填，实际为 0")
		}
		if got := verifiedRaw(t, m.ID); got != "1" {
			t.Errorf("named bool 落库值期望 1，实际 %q", got)
		}
	})

	t.Run("Save_新建分支不触发UDT错误", func(t *testing.T) {
		// 用户复现路径核心：db.Save() 一个零主键的完整对象走 INSERT 分支
		m := Merchant{
			Name:      MerchantName("商户B"),
			Status:    MerchantStatusExistence,
			Verified:  MerchantBool(true),
			CreatedAt: time.Now(),
		}
		result := DB.Save(&m)
		if result.Error != nil {
			t.Fatalf("Save(新建分支) 失败: %v", result.Error)
		}
		if m.ID == 0 {
			t.Fatal("期望 Save 后主键 ID 被回填，实际为 0")
		}

		// 改动字段（含 named bool）后再次 Save，走全字段 UPDATE 分支，同样不得报错。
		// named bool 字段正是修复前触发 UDT 报错的完整对象场景之一。
		m.Status = MerchantStatusDeregistered
		m.Name = MerchantName("商户B-改")
		m.Verified = MerchantBool(false)
		result = DB.Save(&m)
		if result.Error != nil {
			msg := result.Error.Error()
			t.Fatalf("Save(全字段UPDATE分支) 失败: %v", msg)
		}

		// 回读排除 verified 列：named bool 经 struct Scan 受上游 NUMBER→string 语义限制
		//（见 verifiedRaw 注释），其落库值改由 verifiedRaw 断言，避免连带阻断其他字段断言
		var got Merchant
		if err := DB.Omit("verified").First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status != MerchantStatusDeregistered || int(got.Status) != int(MerchantStatusDeregistered) {
			t.Errorf("UPDATE 后 Status 往返不一致：期望 %d，实际 %d",
				int(MerchantStatusDeregistered), int(got.Status))
		}
		if got.Name != MerchantName("商户B-改") {
			t.Errorf("UPDATE 后 Name 往返不一致：期望 商户B-改，实际 %s", string(got.Name))
		}
		// GORM 官方 Save 语义：UPDATE 分支设置 Selects=["*"]，显式选中的列零值也更新，
		// 因此 Verified=false 应落库为 "0"（驱动 Update 回调已对齐该语义）
		if got2 := verifiedRaw(t, m.ID); got2 != "0" {
			t.Errorf("Save 全字段更新后 named bool 零值应落库 0，实际 %q", got2)
		}

		// 原生 Exec 传 named bool 零值参数：直接验证 go-ora 参数编码层对
		// named bool(false) 的编码写入（GORM builder 的零值过滤属框架语义，不在驱动范围）
		if err := DB.Exec("UPDATE TEST_MERCHANTS SET verified = ? WHERE id = ?", MerchantBool(false), m.ID).Error; err != nil {
			t.Fatalf("原生 Exec 写 named bool false 失败: %v", err)
		}
		if got2 := verifiedRaw(t, m.ID); got2 != "0" {
			t.Errorf("原生 Exec 写 false 后落库值期望 0，实际 %q", got2)
		}
	})

	t.Run("First_Find_类型化回读一致", func(t *testing.T) {
		src := Merchant{
			Name:      MerchantName("商户C"),
			Status:    MerchantStatusExistence,
			Verified:  MerchantBool(true),
			CreatedAt: time.Now(),
		}
		if err := DB.Create(&src).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}

		// First 单条回读（排除 verified 列，理由见 verifiedRaw 注释）
		var first Merchant
		if err := DB.Omit("verified").First(&first, src.ID).Error; err != nil {
			t.Fatalf("First 回读失败: %v", err)
		}
		if first.Status != MerchantStatusExistence {
			t.Errorf("Status 类型化往返不一致：期望 %d，实际 %d",
				int(MerchantStatusExistence), int(first.Status))
		}
		if first.Name != MerchantName("商户C") {
			t.Errorf("Name 往返不一致：期望 商户C，实际 %s", string(first.Name))
		}
		if got := verifiedRaw(t, src.ID); got != "1" {
			t.Errorf("named bool 落库值期望 1，实际 %q", got)
		}

		// Find 多条回读
		var all []Merchant
		if err := DB.Omit("verified").Find(&all, "id = ?", src.ID).Error; err != nil {
			t.Fatalf("Find 回读失败: %v", err)
		}
		if len(all) != 1 || all[0].ID != src.ID ||
			all[0].Status != MerchantStatusExistence ||
			int(all[0].Status) != int(MerchantStatusExistence) {
			t.Fatalf("Find 结果与写入不一致：%+v", all)
		}
	})

	t.Run("Updates_Update单列与struct更新", func(t *testing.T) {
		m := Merchant{
			Name:      MerchantName("商户D"),
			Status:    MerchantStatusExistence,
			Verified:  MerchantBool(true),
			CreatedAt: time.Now(),
		}
		if err := DB.Create(&m).Error; err != nil {
			t.Fatalf("Create 失败: %v", err)
		}

		// 方式一：Update 单列写枚举值（named int 与 named bool 各一次）
		if err := DB.Model(&Merchant{}).Where("id = ?", m.ID).
			Update("status", MerchantStatusDeregistered).Error; err != nil {
			t.Fatalf("Update(status) 失败: %v", err)
		}
		if err := DB.Model(&Merchant{}).Where("id = ?", m.ID).
			Update("verified", MerchantBool(false)).Error; err != nil {
			t.Fatalf("Update(verified) 失败: %v", err)
		}
		var got Merchant
		if err := DB.Omit("verified").First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status != MerchantStatusDeregistered {
			t.Errorf("Update(status) 后期望 %d，实际 %d",
				int(MerchantStatusDeregistered), int(got.Status))
		}
		if got2 := verifiedRaw(t, m.ID); got2 != "0" {
			t.Errorf("named bool Update 后落库值期望 0，实际 %q", got2)
		}

		// 方式二：struct Updates（注意 GORM 对 struct 零值字段自动忽略，故使用非零值字段）
		if err := DB.Model(&Merchant{}).Where("id = ?", m.ID).
			Updates(Merchant{Name: MerchantName("商户D-struct"), Status: MerchantStatusExistence, Verified: MerchantBool(true)}).Error; err != nil {
			t.Fatalf("Updates(struct) 失败: %v", err)
		}
		if err := DB.Omit("verified").First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status != MerchantStatusExistence || int(got.Status) != int(MerchantStatusExistence) {
			t.Errorf("Updates(struct) 后 Status 期望 %d，实际 %d",
				int(MerchantStatusExistence), int(got.Status))
		}
		if got.Name != MerchantName("商户D-struct") {
			t.Errorf("Updates(struct) 后 Name 期望 商户D-struct，实际 %s", string(got.Name))
		}
		if got2 := verifiedRaw(t, m.ID); got2 != "1" {
			t.Errorf("named bool struct Updates 后落库值期望 1，实际 %q", got2)
		}
	})

	t.Run("零值边界_Status为0显式保存不被吞掉", func(t *testing.T) {
		// 默认链路下显式保存枚举零值（MerchantStatusCreated = 0）：
		// 记录必须落库且回读为 0，不得变成 NULL 或被 default 吞成缺失。
		// 如实际行为与之不符，本子测试如实暴露结果，不为通过而改生产代码。
		m := Merchant{
			Name:      MerchantName("商户E"),
			Status:    MerchantStatusCreated,
			CreatedAt: time.Now(),
		}
		if err := DB.Create(&m).Error; err != nil {
			t.Fatalf("Create(Status=0) 失败: %v", err)
		}
		if m.ID == 0 {
			t.Fatal("期望 Create 后主键 ID 被回填，实际为 0")
		}

		var got Merchant
		if err := DB.Omit("verified").First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if got.Status != MerchantStatusCreated || int(got.Status) != 0 {
			t.Errorf("零值 Status 往返不一致：期望 %d，实际 %d",
				0, int(got.Status))
		}
		if got2 := verifiedRaw(t, m.ID); got2 != "0" {
			t.Errorf("named bool Create 零值落库期望 0，实际 %q", got2)
		}

		// 再以 map Updates 显式写回零值（绕开 GORM struct Updates 的零值忽略行为）。
		// named bool 的 false 同理：struct Updates 会忽略 false，必须走 map 或 Save 全字段。
		if err := DB.Model(&Merchant{}).Where("id = ?", m.ID).
			Updates(map[string]any{"status": MerchantStatusCreated, "verified": MerchantBool(false)}).Error; err != nil {
			t.Fatalf("map Updates 写入零值失败: %v", err)
		}
		if err := DB.Omit("verified").First(&got, m.ID).Error; err != nil {
			t.Fatalf("回读失败: %v", err)
		}
		if int(got.Status) != 0 {
			t.Errorf("map Updates 后零值 Status 期望 0，实际 %d", int(got.Status))
		}
		if got2 := verifiedRaw(t, m.ID); got2 != "0" {
			t.Errorf("map Updates 后零值 named bool 期望 0，实际 %q", got2)
		}
	})
}

// TestPrepareStmtEnumRoundTrip 验证 prepared 路径（gorm.Config{PrepareStmt: true}）
// 下枚举规范化的 Stmt 级包装生效。
//
// database/sql 对 prepared 语句优先使用 Stmt 的 NamedValueChecker
// （convert.go: nvc, ok := si.(driver.NamedValueChecker); if !ok { nvc, _ = ci.(...) }），
// go-ora 的 *Stmt 实现了 CheckNamedValue，若只包装连接层，prepared 路径会绕过
// 连接级规范化，枚举字段坠入 UDT 分支报错。enum_normalize.go 新增的
// enumNormalizeStmt 包装修复该缺口：若规范化缺失，本测试的 Create 或 Save
// 步骤会报 UDT 错误。
func TestPrepareStmtEnumRoundTrip(t *testing.T) {
	db := openPreparedDB(t)
	if err := db.AutoMigrate(&Merchant{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	clearTable(t, "TEST_MERCHANTS")

	// Create 完整对象（含枚举字段非零值）
	m := Merchant{
		Name:      MerchantName("商户P"),
		Status:    MerchantStatusExistence,
		Verified:  MerchantBool(true),
		CreatedAt: time.Now(),
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("prepared Create(含枚举字段) 失败: %v", err)
	}
	if m.ID == 0 {
		t.Fatal("期望 prepared Create 后主键 ID 被回填，实际为 0")
	}

	// 修改枚举字段后 Save 走全字段 UPDATE 分支（prepared 路径）
	m.Status = MerchantStatusDeregistered
	m.Name = MerchantName("商户P-改")
	m.Verified = MerchantBool(false)
	if err := db.Save(&m).Error; err != nil {
		t.Fatalf("prepared Save(全字段UPDATE) 失败: %v", err)
	}

	// 回读排除 verified 列：named bool 经 struct Scan 受上游 NUMBER→string
	// 语义限制（见 verifiedRaw 注释），其落库值改由 verifiedRaw 断言
	var got Merchant
	if err := db.Omit("verified").First(&got, m.ID).Error; err != nil {
		t.Fatalf("prepared 回读失败: %v", err)
	}
	if got.Status != MerchantStatusDeregistered || int(got.Status) != int(MerchantStatusDeregistered) {
		t.Errorf("prepared UPDATE 后 Status 往返不一致：期望 %d，实际 %d",
			int(MerchantStatusDeregistered), int(got.Status))
	}
	if got.Name != MerchantName("商户P-改") {
		t.Errorf("prepared UPDATE 后 Name 往返不一致：期望 商户P-改，实际 %s", string(got.Name))
	}
	if got2 := verifiedRaw(t, m.ID); got2 != "0" {
		t.Errorf("prepared Save 后 named bool 零值应落库 0，实际 %q", got2)
	}
}
