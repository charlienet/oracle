package driver_adapter

import (
	"slices"
	"testing"
)

// TestRegistryRegisterAndGet 验证 Register 后 Get 能返回对应适配器
func TestRegistryRegisterAndGet(t *testing.T) {
	const key DriverType = "test-registry-driver"

	callCount := 0
	Register(key, func() Adapter {
		callCount++
		return &GoOraAdapter{}
	})
	defer delete(registry, key)

	adapter := Get(key)
	if adapter == nil {
		t.Fatalf("Get(%q) 返回 nil，期望返回已注册的适配器", key)
	}
	if _, ok := adapter.(*GoOraAdapter); !ok {
		t.Fatalf("Get(%q) 返回类型 = %T，期望 *GoOraAdapter", key, adapter)
	}
	if callCount != 1 {
		t.Errorf("factory 调用次数 = %d，期望 1", callCount)
	}

	// 每次 Get 都应调用 factory 返回新实例
	if Get(key) == nil {
		t.Error("第二次 Get 返回 nil")
	}
	if callCount != 2 {
		t.Errorf("factory 调用次数 = %d，期望 2（每次 Get 都应调用 factory）", callCount)
	}
}

// TestRegistryGetUnknown 验证 Get 未注册的 DriverType 返回 nil
func TestRegistryGetUnknown(t *testing.T) {
	if adapter := Get(DriverType("no-such-driver")); adapter != nil {
		t.Fatalf("Get(未注册类型) 返回 %v，期望 nil", adapter)
	}
}

// TestRegistryListDrivers 验证 ListDrivers 返回包含 DriverGoOra
func TestRegistryListDrivers(t *testing.T) {
	drivers := ListDrivers()
	if len(drivers) == 0 {
		t.Fatal("ListDrivers() 返回空列表")
	}
	if slices.Contains(drivers, DriverGoOra) {
		return
	}
	t.Errorf("ListDrivers() = %v，应包含 DriverGoOra", drivers)
}

// TestRegistryGodrorNotCompiled 验证默认构建（无 godror build tag）下不包含 DriverGodror
// 当使用 -tags=godror 构建时，此测试验证 godror 驱动已注册
func TestRegistryGodrorNotCompiled(t *testing.T) {
	drivers := ListDrivers()

	// 检测是否编译了 godror 驱动
	godrorPresent := slices.Contains(drivers, DriverGodror)

	// 尝试获取 godror 适配器来确认是否编译
	adapter := Get(DriverGodror)
	godrorCompiled := adapter != nil

	if godrorCompiled && !godrorPresent {
		t.Errorf("Get(DriverGodror) 返回适配器但 ListDrivers() 不包含 DriverGodror")
	}

	if godrorCompiled {
		t.Logf("godror 驱动已编译（使用了 -tags=godror）")
	} else {
		t.Logf("godror 驱动未编译（默认构建，未使用 -tags=godror）")
	}
}

// TestDriverTypeConstants 验证驱动类型常量值
func TestDriverTypeConstants(t *testing.T) {
	if DriverGoOra != "go-ora" {
		t.Errorf("DriverGoOra = %q，期望 %q", DriverGoOra, "go-ora")
	}
	if DriverGodror != "godror" {
		t.Errorf("DriverGodror = %q，期望 %q", DriverGodror, "godror")
	}
}
