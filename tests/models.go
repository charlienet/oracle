package tests

import (
	"time"

	"gorm.io/gorm"
)

// User 基本测试模型
type User struct {
	ID        uint           `gorm:"column:id;primaryKey;autoIncrement"`
	Name      string         `gorm:"size:100;not null"`
	Email     string         `gorm:"size:200;uniqueIndex"`
	Age       int            `gorm:"default:0"`
	Active    bool           // 不使用 default，以便显式设置 false 时能正确存储
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (User) TableName() string {
	return "TEST_USERS"
}

// Product 测试数值类型
type Product struct {
	ID          uint    `gorm:"column:id;primaryKey;autoIncrement"`
	Name        string  `gorm:"size:200;not null"`
	Price       float64 `gorm:"precision:10;scale:2"`
	Stock       int     `gorm:"default:0"`
	Description string  `gorm:"type:CLOB"`
	CreatedAt   time.Time
}

func (Product) TableName() string {
	return "TEST_PRODUCTS"
}

// Order 测试关联关系
type Order struct {
	ID        uint      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint      `gorm:"not null;index"`
	User      User      `gorm:"foreignKey:UserID"`
	Total     float64   `gorm:"precision:12;scale:2"`
	Status    string    `gorm:"size:20;default:'pending'"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Order) TableName() string {
	return "TEST_ORDERS"
}

// SeqDefaultModel 显式使用序列默认值的模型（主键不用 autoIncrement，
// 而是通过列的 DEFAULT 值使用序列 SEQ_TEST_SEQ_DEFAULT.NEXTVAL）
type SeqDefaultModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Name string `gorm:"size:100"`
}

func (SeqDefaultModel) TableName() string {
	return "TEST_SEQ_DEFAULT"
}

// SeqDefaultViaDriverModel 通过驱动 AutoMigrate 建表，验证 11g 下序列默认值的
// 触发器路径（模型使用 gorm:"default:(SEQ_TEST_SEQ_DEF_CODE.NEXTVAL)"）。
//
// Code 用 int 字段并给默认值加括号：GORM 对含括号的默认值会跳过 ParseInt 解析
// （schema/field.go:231），从而 DefaultValueInterface 保持 nil，字段进入
// FieldsWithDefaultDBValue —— INSERT 时 GORM 会省略该列，触发 BEFORE INSERT 触发器回填序列值。
// 若不加括号，GORM 会把 "SEQ_...NEXTVAL" 当作整数解析失败，schema.Parse 直接报错。
type SeqDefaultViaDriverModel struct {
	ID   uint   `gorm:"column:id;primaryKey"`
	Code int    `gorm:"column:code;default:(SEQ_TEST_SEQ_DEF_CODE.NEXTVAL)"`
	Name string `gorm:"size:100"`
}

func (SeqDefaultViaDriverModel) TableName() string {
	return "TEST_SEQ_DEF"
}
