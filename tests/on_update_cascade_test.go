package tests

import (
	"testing"
)

// TestOnUpdateCascadeTrigger 行为验证：ON UPDATE CASCADE 触发器真正生效。
// 修复前 createOnUpdateTrigger 用 rel.Field.DBName（belongs_to 下为空）作子表外键列，
// 触发器从未创建（warn "identifier cannot be empty"）；修复后改用
// constraint.ForeignKeys[0].DBName（子表外键列），此处验证级联语义：
// 更新父表主键后，子表外键列应随之更新。
func TestOnUpdateCascadeTrigger(t *testing.T) {
	type cascadeParent struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}
	type cascadeChild struct {
		ID       uint `gorm:"primaryKey"`
		ParentID uint
		Parent   cascadeParent `gorm:"foreignKey:ParentID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL"`
	}

	_ = DB.Migrator().DropTable(&cascadeChild{}, &cascadeParent{})
	if err := DB.AutoMigrate(&cascadeParent{}, &cascadeChild{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&cascadeChild{}, &cascadeParent{}) }()
	parentTable := DB.NamingStrategy.TableName("cascadeParent")
	childTable := DB.NamingStrategy.TableName("cascadeChild")

	p := cascadeParent{Name: "p1"}
	if err := DB.Create(&p).Error; err != nil {
		t.Fatalf("failed to create parent: %v", err)
	}
	c := cascadeChild{ParentID: p.ID}
	if err := DB.Create(&c).Error; err != nil {
		t.Fatalf("failed to create child: %v", err)
	}

	// 更新父表主键 1 -> 100，子表外键应级联更新为 100
	newID := p.ID + 99
	if err := DB.Exec("UPDATE "+parentTable+" SET ID = ? WHERE ID = ?", newID, p.ID).Error; err != nil {
		t.Fatalf("failed to update parent id: %v", err)
	}

	var gotParentID uint
	if err := DB.Raw("SELECT PARENT_ID FROM "+childTable+" WHERE ID = ?", c.ID).Scan(&gotParentID).Error; err != nil {
		t.Fatalf("failed to read child: %v", err)
	}
	if gotParentID != newID {
		t.Errorf("ON UPDATE CASCADE 未生效：子表 PARENT_ID 期望 %d，实际 %d", newID, gotParentID)
	}
}
