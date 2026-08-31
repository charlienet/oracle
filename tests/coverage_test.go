package tests

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ========== Migrator 功能测试 ==========

// TestMigratorCurrentDatabase 测试获取当前数据库名称
func TestMigratorCurrentDatabase(t *testing.T) {
	m := DB.Migrator()
	if dm, ok := m.(interface {
		CurrentDatabase() string
	}); ok {
		name := dm.CurrentDatabase()
		if name == "" {
			t.Error("CurrentDatabase returned empty string")
		}
		t.Logf("Current database: %s", name)
	} else {
		t.Skip("Migrator does not implement CurrentDatabase")
	}
}

// TestMigratorDropColumn 测试删除列
func TestMigratorDropColumn(t *testing.T) {
	type DropColumnModel struct {
		ID    uint   `gorm:"primaryKey"`
		Name  string `gorm:"size:100"`
		Age   int
		Email string `gorm:"size:200"`
	}

	_ = DB.Migrator().DropTable(&DropColumnModel{})
	if err := DB.AutoMigrate(&DropColumnModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&DropColumnModel{}) }()

	// 验证列存在
	if !DB.Migrator().HasColumn(&DropColumnModel{}, "Age") {
		t.Fatal("Age column should exist before drop")
	}

	// 删除列
	if err := DB.Migrator().DropColumn(&DropColumnModel{}, "Age"); err != nil {
		t.Fatalf("failed to drop column: %v", err)
	}

	// 验证列已删除
	if DB.Migrator().HasColumn(&DropColumnModel{}, "Age") {
		t.Error("Age column should not exist after drop")
	}
}

// TestMigratorAlterColumn 测试修改列
func TestMigratorAlterColumn(t *testing.T) {
	type AlterColumnModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:50"`
	}

	_ = DB.Migrator().DropTable(&AlterColumnModel{})
	if err := DB.AutoMigrate(&AlterColumnModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&AlterColumnModel{}) }()

	// 修改列大小
	type AlterColumnModelNew struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:200"`
	}

	if err := DB.Migrator().AlterColumn(&AlterColumnModelNew{}, "Name"); err != nil {
		t.Fatalf("failed to alter column: %v", err)
	}
}

// TestMigratorHasColumn 测试检查列是否存在
func TestMigratorHasColumn(t *testing.T) {
	type HasColumnModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&HasColumnModel{})
	if err := DB.AutoMigrate(&HasColumnModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&HasColumnModel{}) }()

	if !DB.Migrator().HasColumn(&HasColumnModel{}, "Name") {
		t.Error("Name column should exist")
	}

	if DB.Migrator().HasColumn(&HasColumnModel{}, "NonExistent") {
		t.Error("NonExistent column should not exist")
	}
}

// TestMigratorCreateConstraint 测试创建约束
// 断言：AutoMigrate 后外键约束真实存在（GORM 默认名 fk_<表>_<字段>，Oracle 存储为大写）
func TestMigratorCreateConstraint(t *testing.T) {
	type ParentModel struct {
		ID uint `gorm:"primaryKey"`
	}

	type ChildModel struct {
		ID       uint `gorm:"primaryKey"`
		ParentID uint
		Parent   ParentModel `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	}

	_ = DB.Migrator().DropTable(&ChildModel{}, &ParentModel{})
	if err := DB.AutoMigrate(&ParentModel{}, &ChildModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&ChildModel{}, &ParentModel{}) }()

	// 验证外键约束已创建
	if !DB.Migrator().HasConstraint(&ChildModel{}, "FK_CHILD_MODELS_PARENT") {
		t.Error("expected foreign key constraint FK_CHILD_MODELS_PARENT to exist after AutoMigrate")
	}
}

// TestMigratorDropConstraint 测试删除约束
// 断言：删除前约束存在，删除后约束不存在
func TestMigratorDropConstraint(t *testing.T) {
	type ParentModel2 struct {
		ID uint `gorm:"primaryKey"`
	}

	type ChildModel2 struct {
		ID       uint `gorm:"primaryKey"`
		ParentID uint
		Parent   ParentModel2 `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	}

	_ = DB.Migrator().DropTable(&ChildModel2{}, &ParentModel2{})
	if err := DB.AutoMigrate(&ParentModel2{}, &ChildModel2{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&ChildModel2{}, &ParentModel2{}) }()

	// 先确认约束存在
	if !DB.Migrator().HasConstraint(&ChildModel2{}, "FK_CHILD_MODEL2_PARENT") {
		t.Fatal("expected foreign key constraint FK_CHILD_MODEL2_PARENT to exist before drop")
	}

	// 删除约束
	if err := DB.Migrator().DropConstraint(&ChildModel2{}, "FK_CHILD_MODEL2_PARENT"); err != nil {
		t.Fatalf("failed to drop constraint: %v", err)
	}

	// 验证约束已删除
	if DB.Migrator().HasConstraint(&ChildModel2{}, "FK_CHILD_MODEL2_PARENT") {
		t.Error("constraint FK_CHILD_MODEL2_PARENT should not exist after drop")
	}
}

// TestMigratorHasConstraint 测试检查约束是否存在
// 断言：AutoMigrate 创建外键后 HasConstraint 返回 true
func TestMigratorHasConstraint(t *testing.T) {
	type ParentModel3 struct {
		ID uint `gorm:"primaryKey"`
	}

	type ChildModel3 struct {
		ID       uint `gorm:"primaryKey"`
		ParentID uint
		Parent   ParentModel3 `gorm:"constraint:OnUpdate:CASCADE,OnDelete:SET NULL;"`
	}

	_ = DB.Migrator().DropTable(&ChildModel3{}, &ParentModel3{})
	if err := DB.AutoMigrate(&ParentModel3{}, &ChildModel3{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&ChildModel3{}, &ParentModel3{}) }()

	// AutoMigrate 后外键约束应存在
	if !DB.Migrator().HasConstraint(&ChildModel3{}, "FK_CHILD_MODEL3_PARENT") {
		t.Error("expected foreign key constraint FK_CHILD_MODEL3_PARENT to exist")
	}
}

// TestMigratorDropIndex 测试删除索引
func TestMigratorDropIndex(t *testing.T) {
	type IndexModel struct {
		ID    uint   `gorm:"primaryKey"`
		Email string `gorm:"index:idx_email"`
	}

	_ = DB.Migrator().DropTable(&IndexModel{})
	if err := DB.AutoMigrate(&IndexModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&IndexModel{}) }()

	// 验证索引存在
	if !DB.Migrator().HasIndex(&IndexModel{}, "idx_email") {
		t.Fatal("Index should exist before drop")
	}

	// 删除索引
	if err := DB.Migrator().DropIndex(&IndexModel{}, "idx_email"); err != nil {
		t.Fatalf("failed to drop index: %v", err)
	}

	// 验证索引已删除
	if DB.Migrator().HasIndex(&IndexModel{}, "idx_email") {
		t.Error("Index should not exist after drop")
	}
}

// TestMigratorRenameIndex 测试重命名索引
func TestMigratorRenameIndex(t *testing.T) {
	type RenameIndexModel struct {
		ID    uint   `gorm:"primaryKey"`
		Email string `gorm:"index:idx_old"`
	}

	_ = DB.Migrator().DropTable(&RenameIndexModel{})
	if err := DB.AutoMigrate(&RenameIndexModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&RenameIndexModel{}) }()

	// 重命名索引
	if err := DB.Migrator().RenameIndex(&RenameIndexModel{}, "idx_old", "idx_new"); err != nil {
		t.Fatalf("failed to rename index: %v", err)
	}

	// 验证新索引存在
	if !DB.Migrator().HasIndex(&RenameIndexModel{}, "idx_new") {
		t.Error("New index should exist after rename")
	}
}

// ========== Oracle 特有功能测试 ==========

// TestOracleSavePoint 测试保存点功能
func TestOracleSavePoint(t *testing.T) {
	type SavePointModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&SavePointModel{})
	if err := DB.AutoMigrate(&SavePointModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&SavePointModel{}) }()

	err := DB.Transaction(func(tx *gorm.DB) error {
		// 创建记录
		if err := tx.Create(&SavePointModel{Name: "Before SavePoint"}).Error; err != nil {
			return err
		}

		// 创建保存点
		if err := tx.SavePoint("sp1").Error; err != nil {
			return err
		}

		// 创建更多记录
		if err := tx.Create(&SavePointModel{Name: "After SavePoint"}).Error; err != nil {
			return err
		}

		// 回滚到保存点
		if err := tx.RollbackTo("sp1").Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf("transaction with savepoint failed: %v", err)
	}

	// 验证只有第一条记录存在
	var count int64
	DB.Model(&SavePointModel{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record after rollback to savepoint, got %d", count)
	}
}

// TestOracleRewriteLimit 测试分页重写（12c+）
func TestOracleRewriteLimit(t *testing.T) {
	type LimitModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&LimitModel{})
	if err := DB.AutoMigrate(&LimitModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&LimitModel{}) }()

	// 创建测试数据
	for range 20 {
		DB.Create(&LimitModel{Name: "Item"})
	}

	// 测试 Limit
	var results []LimitModel
	if err := DB.Limit(5).Find(&results).Error; err != nil {
		t.Fatalf("failed to query with limit: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}

	// 测试 Offset + Limit
	results = nil
	if err := DB.Offset(5).Limit(5).Find(&results).Error; err != nil {
		t.Fatalf("failed to query with offset and limit: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}

// ========== Query 功能测试 ==========

// TestQueryOrderBy 测试排序查询
func TestQueryOrderBy(t *testing.T) {
	type OrderByModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
		Age  int
	}

	_ = DB.Migrator().DropTable(&OrderByModel{})
	if err := DB.AutoMigrate(&OrderByModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&OrderByModel{}) }()

	// 创建测试数据
	DB.Create(&OrderByModel{Name: "Alice", Age: 30})
	DB.Create(&OrderByModel{Name: "Bob", Age: 25})
	DB.Create(&OrderByModel{Name: "Charlie", Age: 35})

	// 测试升序
	var results []OrderByModel
	if err := DB.Order("age ASC").Find(&results).Error; err != nil {
		t.Fatalf("failed to query with order: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	if results[0].Age != 25 {
		t.Errorf("expected first result age 25, got %d", results[0].Age)
	}

	// 测试降序
	results = nil
	if err := DB.Order("age DESC").Find(&results).Error; err != nil {
		t.Fatalf("failed to query with order desc: %v", err)
	}
	if results[0].Age != 35 {
		t.Errorf("expected first result age 35, got %d", results[0].Age)
	}
}

// TestQueryGroupBy 测试分组查询
func TestQueryGroupBy(t *testing.T) {
	type GroupModel struct {
		ID       uint   `gorm:"primaryKey"`
		Category string `gorm:"size:50"`
		Value    int
	}

	_ = DB.Migrator().DropTable(&GroupModel{})
	if err := DB.AutoMigrate(&GroupModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&GroupModel{}) }()

	// 创建测试数据
	DB.Create(&GroupModel{Category: "A", Value: 10})
	DB.Create(&GroupModel{Category: "A", Value: 20})
	DB.Create(&GroupModel{Category: "B", Value: 30})

	// 测试分组查询
	type Result struct {
		Category string
		Total    int
	}
	var results []Result
	if err := DB.Model(&GroupModel{}).Select("category, SUM(value) as total").Group("category").Find(&results).Error; err != nil {
		t.Fatalf("failed to query with group by: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 groups, got %d", len(results))
	}
}

// TestQueryDistinct 测试去重查询
func TestQueryDistinct(t *testing.T) {
	type DistinctModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&DistinctModel{})
	if err := DB.AutoMigrate(&DistinctModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&DistinctModel{}) }()

	// 创建重复数据
	DB.Create(&DistinctModel{Name: "Alice"})
	DB.Create(&DistinctModel{Name: "Alice"})
	DB.Create(&DistinctModel{Name: "Bob"})

	// 测试去重查询
	var names []string
	if err := DB.Model(&DistinctModel{}).Distinct("name").Pluck("name", &names).Error; err != nil {
		t.Fatalf("failed to query distinct: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("expected 2 distinct names, got %d", len(names))
	}
}

// ========== Update 功能测试 ==========

// TestUpdateWithMap 测试使用 map 更新
func TestUpdateWithMap(t *testing.T) {
	type MapUpdateModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
		Age  int
	}

	_ = DB.Migrator().DropTable(&MapUpdateModel{})
	if err := DB.AutoMigrate(&MapUpdateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&MapUpdateModel{}) }()

	// 创建记录
	model := MapUpdateModel{Name: "Original", Age: 20}
	DB.Create(&model)

	// 使用 map 更新
	if err := DB.Model(&model).Updates(map[string]any{
		"name": "Updated",
		"age":  30,
	}).Error; err != nil {
		t.Fatalf("failed to update with map: %v", err)
	}

	// 验证更新
	var updated MapUpdateModel
	DB.First(&updated, model.ID)
	if updated.Name != "Updated" || updated.Age != 30 {
		t.Errorf("update with map failed: got name=%s, age=%d", updated.Name, updated.Age)
	}
}

// TestUpdateWithStruct 测试使用 struct 更新
func TestUpdateWithStruct(t *testing.T) {
	type StructUpdateModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
		Age  int
	}

	_ = DB.Migrator().DropTable(&StructUpdateModel{})
	if err := DB.AutoMigrate(&StructUpdateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&StructUpdateModel{}) }()

	// 创建记录
	model := StructUpdateModel{Name: "Original", Age: 20}
	DB.Create(&model)

	// 使用 struct 更新（只更新非零值）
	if err := DB.Model(&model).Updates(StructUpdateModel{Name: "Updated", Age: 0}).Error; err != nil {
		t.Fatalf("failed to update with struct: %v", err)
	}

	// 验证更新（Age 应该保持原值，因为 0 是零值）
	var updated StructUpdateModel
	DB.First(&updated, model.ID)
	if updated.Name != "Updated" {
		t.Errorf("update with struct failed: got name=%s", updated.Name)
	}
}

// TestUpdateColumns 测试指定列更新
func TestUpdateColumns(t *testing.T) {
	type ColumnsUpdateModel struct {
		ID    uint   `gorm:"primaryKey"`
		Name  string `gorm:"size:100"`
		Age   int
		Email string `gorm:"size:200"`
	}

	_ = DB.Migrator().DropTable(&ColumnsUpdateModel{})
	if err := DB.AutoMigrate(&ColumnsUpdateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&ColumnsUpdateModel{}) }()

	// 创建记录
	model := ColumnsUpdateModel{Name: "Original", Age: 20, Email: "old@example.com"}
	DB.Create(&model)

	// 只更新指定列
	if err := DB.Model(&model).Select("name", "age").Updates(map[string]any{
		"name":  "Updated",
		"age":   30,
		"email": "new@example.com",
	}).Error; err != nil {
		t.Fatalf("failed to update with select: %v", err)
	}

	// 验证更新（email 应该保持原值）
	var updated ColumnsUpdateModel
	DB.First(&updated, model.ID)
	if updated.Name != "Updated" || updated.Age != 30 {
		t.Errorf("update failed: got name=%s, age=%d", updated.Name, updated.Age)
	}
	if updated.Email != "old@example.com" {
		t.Errorf("email should not be updated: got %s", updated.Email)
	}
}

// ========== Delete 功能测试 ==========

// TestDeleteBatch 测试批量删除
func TestDeleteBatch(t *testing.T) {
	type BatchDeleteModel struct {
		ID       uint   `gorm:"primaryKey"`
		Category string `gorm:"size:50"`
	}

	_ = DB.Migrator().DropTable(&BatchDeleteModel{})
	if err := DB.AutoMigrate(&BatchDeleteModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&BatchDeleteModel{}) }()

	// 创建测试数据
	for range 10 {
		DB.Create(&BatchDeleteModel{Category: "A"})
	}
	for range 5 {
		DB.Create(&BatchDeleteModel{Category: "B"})
	}

	// 批量删除
	if err := DB.Where("category = ?", "A").Delete(&BatchDeleteModel{}).Error; err != nil {
		t.Fatalf("failed to batch delete: %v", err)
	}

	// 验证删除
	var count int64
	DB.Model(&BatchDeleteModel{}).Where("category = ?", "A").Count(&count)
	if count != 0 {
		t.Errorf("expected 0 records after delete, got %d", count)
	}

	DB.Model(&BatchDeleteModel{}).Where("category = ?", "B").Count(&count)
	if count != 5 {
		t.Errorf("expected 5 records remaining, got %d", count)
	}
}

// TestDeleteWithConditions 测试条件删除
func TestDeleteWithConditions(t *testing.T) {
	type ConditionDeleteModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
		Age  int
	}

	_ = DB.Migrator().DropTable(&ConditionDeleteModel{})
	if err := DB.AutoMigrate(&ConditionDeleteModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&ConditionDeleteModel{}) }()

	// 创建测试数据
	DB.Create(&ConditionDeleteModel{Name: "Alice", Age: 25})
	DB.Create(&ConditionDeleteModel{Name: "Bob", Age: 30})
	DB.Create(&ConditionDeleteModel{Name: "Charlie", Age: 35})

	// 条件删除
	if err := DB.Where("age > ?", 28).Delete(&ConditionDeleteModel{}).Error; err != nil {
		t.Fatalf("failed to delete with conditions: %v", err)
	}

	// 验证删除
	var count int64
	DB.Model(&ConditionDeleteModel{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record remaining, got %d", count)
	}
}

// ========== Create 功能测试 ==========

// TestCreateWithMap 测试使用 map 创建
func TestCreateWithMap(t *testing.T) {
	type MapCreateModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
		Age  int
	}

	_ = DB.Migrator().DropTable(&MapCreateModel{})
	if err := DB.AutoMigrate(&MapCreateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&MapCreateModel{}) }()

	// 使用 map 创建
	data := map[string]any{
		"name": "Test",
		"age":  25,
	}
	if err := DB.Model(&MapCreateModel{}).Create(data).Error; err != nil {
		t.Fatalf("failed to create with map: %v", err)
	}

	// 验证创建
	var count int64
	DB.Model(&MapCreateModel{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record, got %d", count)
	}
}

// TestCreateInBatches 测试批量创建
func TestCreateInBatches(t *testing.T) {
	type BatchCreateModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&BatchCreateModel{})
	if err := DB.AutoMigrate(&BatchCreateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&BatchCreateModel{}) }()

	// 批量创建
	records := make([]BatchCreateModel, 100)
	for i := range records {
		records[i] = BatchCreateModel{Name: "Batch"}
	}

	if err := DB.CreateInBatches(records, 10).Error; err != nil {
		t.Fatalf("failed to create in batches: %v", err)
	}

	// 验证创建
	var count int64
	DB.Model(&BatchCreateModel{}).Count(&count)
	if count != 100 {
		t.Errorf("expected 100 records, got %d", count)
	}
}

// TestCreateWithOmit 测试 Omit 创建
func TestCreateWithOmit(t *testing.T) {
	type OmitCreateModel struct {
		ID    uint   `gorm:"primaryKey"`
		Name  string `gorm:"size:100"`
		Email string `gorm:"size:200"`
		Age   int
	}

	_ = DB.Migrator().DropTable(&OmitCreateModel{})
	if err := DB.AutoMigrate(&OmitCreateModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&OmitCreateModel{}) }()

	// 创建时忽略某些字段
	model := OmitCreateModel{Name: "Test", Email: "test@example.com", Age: 25}
	if err := DB.Omit("Email", "Age").Create(&model).Error; err != nil {
		t.Fatalf("failed to create with omit: %v", err)
	}

	// 验证创建（Email 和 Age 应该是零值）
	var created OmitCreateModel
	DB.First(&created, model.ID)
	if created.Name != "Test" {
		t.Errorf("name should be set: got %s", created.Name)
	}
	if created.Email != "" {
		t.Errorf("email should be empty: got %s", created.Email)
	}
	if created.Age != 0 {
		t.Errorf("age should be 0: got %d", created.Age)
	}
}

// ========== 事务测试 ==========

// TestTransaction 测试事务
func TestTransaction(t *testing.T) {
	type TransactionModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&TransactionModel{})
	if err := DB.AutoMigrate(&TransactionModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&TransactionModel{}) }()

	// 测试事务提交
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&TransactionModel{Name: "Tx1"}).Error; err != nil {
			return err
		}
		if err := tx.Create(&TransactionModel{Name: "Tx2"}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	var count int64
	DB.Model(&TransactionModel{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 records after transaction, got %d", count)
	}

	// 测试事务回滚
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&TransactionModel{Name: "Tx3"}).Error; err != nil {
			return err
		}
		return gorm.ErrRecordNotFound // 触发回滚
	})
	if err == nil {
		t.Error("expected transaction to fail")
	}

	DB.Model(&TransactionModel{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 records after rollback, got %d", count)
	}
}

// ========== OnConflict 测试 ==========

// TestOnConflictDoNothing 测试冲突时不更新
func TestOnConflictDoNothing(t *testing.T) {
	type DoNothingModel struct {
		ID   uint   `gorm:"primaryKey"`
		Name string `gorm:"size:100"`
	}

	_ = DB.Migrator().DropTable(&DoNothingModel{})
	if err := DB.AutoMigrate(&DoNothingModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&DoNothingModel{}) }()

	// 创建初始记录
	model := DoNothingModel{ID: 1, Name: "Original"}
	DB.Create(&model)

	// 冲突时不更新
	conflict := DoNothingModel{ID: 1, Name: "Updated"}
	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&conflict)
	if result.Error != nil {
		t.Fatalf("on conflict do nothing failed: %v", result.Error)
	}

	// 验证记录未更新
	var found DoNothingModel
	DB.First(&found, 1)
	if found.Name != "Original" {
		t.Errorf("name should not be updated: got %s", found.Name)
	}
}

// ========== 软删除测试 ==========

// TestSoftDeleteRecover 测试恢复软删除记录
func TestSoftDeleteRecover(t *testing.T) {
	type RecoverModel struct {
		ID        uint           `gorm:"primaryKey"`
		Name      string         `gorm:"size:100"`
		DeletedAt gorm.DeletedAt `gorm:"index"`
	}

	_ = DB.Migrator().DropTable(&RecoverModel{})
	if err := DB.AutoMigrate(&RecoverModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&RecoverModel{}) }()

	// 创建并软删除
	model := RecoverModel{Name: "Test"}
	DB.Create(&model)
	DB.Delete(&model)

	// 验证软删除
	var count int64
	DB.Model(&RecoverModel{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 records after soft delete, got %d", count)
	}

	// 恢复记录
	if err := DB.Unscoped().Model(&RecoverModel{}).Where("id = ?", model.ID).Update("deleted_at", nil).Error; err != nil {
		t.Fatalf("failed to recover: %v", err)
	}

	// 验证恢复
	DB.Model(&RecoverModel{}).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 record after recover, got %d", count)
	}
}

// ========== 时间戳测试 ==========

// TestTimestampAutoUpdate 测试时间戳自动更新
func TestTimestampAutoUpdate(t *testing.T) {
	type TimestampModel struct {
		ID        uint   `gorm:"primaryKey"`
		Name      string `gorm:"size:100"`
		CreatedAt time.Time
		UpdatedAt time.Time
	}

	_ = DB.Migrator().DropTable(&TimestampModel{})
	if err := DB.AutoMigrate(&TimestampModel{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	defer func() { _ = DB.Migrator().DropTable(&TimestampModel{}) }()

	// 创建记录
	model := TimestampModel{Name: "Test"}
	DB.Create(&model)

	// 从 DB 重新读取，作为基准（解决 Go 纳秒精度 vs Oracle 毫秒精度问题）
	var baseline TimestampModel
	DB.First(&baseline, model.ID)

	originalCreatedAt := baseline.CreatedAt
	originalUpdatedAt := baseline.UpdatedAt

	// 等待一秒确保时间戳不同
	time.Sleep(1 * time.Second)

	// 更新记录
	DB.Model(&model).Update("name", "Updated")

	// 验证时间戳
	var updated TimestampModel
	DB.First(&updated, model.ID)

	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("created_at should not change, got %v, want %v", updated.CreatedAt, originalCreatedAt)
	}
	if updated.UpdatedAt.Equal(originalUpdatedAt) {
		t.Error("updated_at should change")
	}
}
