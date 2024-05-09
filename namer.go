package oracle

import (
	"fmt"
	"strings"

	"gorm.io/gorm/schema"
)

var _ schema.Namer = Namer{}

type Namer struct {
	NamingStrategy schema.Namer
	DBName         string
}

func (n Namer) ConvertNameToFormat(x string) string {
	return strings.ToUpper(x)
}

func (n Namer) TableName(table string) (name string) {
	println("TableName", table)

	tableName := n.ConvertNameToFormat(n.NamingStrategy.TableName(table))
	if len(n.DBName) > 0 {
		return fmt.Sprintf("%s.%s", n.DBName, tableName)
	}
	return tableName
}

func (n Namer) ColumnName(table, column string) (name string) {
	return n.ConvertNameToFormat(n.NamingStrategy.ColumnName(table, column))
}

func (n Namer) JoinTableName(table string) (name string) {
	return n.ConvertNameToFormat(n.NamingStrategy.JoinTableName(table))
}

func (n Namer) RelationshipFKName(relationship schema.Relationship) (name string) {
	return n.ConvertNameToFormat(n.NamingStrategy.RelationshipFKName(relationship))
}

func (n Namer) CheckerName(table, column string) (name string) {
	return n.ConvertNameToFormat(n.NamingStrategy.CheckerName(table, column))
}

func (n Namer) IndexName(table, column string) (name string) {
	return n.ConvertNameToFormat(n.NamingStrategy.IndexName(table, column))
}

func (n Namer) SchemaName(table string) string {
	println("SchemaName", table)
	return n.ConvertNameToFormat(n.NamingStrategy.SchemaName(table))
}

func (n Namer) UniqueName(table, column string) string {
	println("UniqueName", table, column)
	return n.ConvertNameToFormat(n.NamingStrategy.UniqueName(table, column))
}
