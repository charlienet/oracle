package clauses

import (
	"strconv"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// testDialector 是仅用于 SQL 生成测试的最小 Dialector 实现，无需数据库连接。
// 它模拟 Oracle 的绑定变量占位符（:N）和不加引号的标识符引用。
type testDialector struct{}

func (testDialector) Name() string                                   { return "oracle" }
func (testDialector) Initialize(*gorm.DB) error                      { return nil }
func (testDialector) Migrator(*gorm.DB) gorm.Migrator                { return nil }
func (testDialector) DataTypeOf(*schema.Field) string                { return "" }
func (testDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }

func (testDialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v any) {
	_, _ = writer.WriteString(":")
	_, _ = writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (testDialector) QuoteTo(writer clause.Writer, str string) {
	_, _ = writer.WriteString(str)
}

func (testDialector) Explain(sql string, vars ...any) string { return sql }

// newStatement 构造一个可以直接作为 clause.Builder 使用的 gorm.Statement。
func newStatement(t *testing.T) *gorm.Statement {
	t.Helper()
	db := &gorm.DB{
		Config: &gorm.Config{Dialector: testDialector{}},
	}
	return &gorm.Statement{DB: db, Table: "users"}
}

// buildSQL 直接调用子句的 Build 方法生成 SQL。
func buildSQL(t *testing.T, expr clause.Expression) string {
	t.Helper()
	stmt := newStatement(t)
	expr.Build(stmt)
	return stmt.SQL.String()
}

// buildClauseSQL 通过 clause.Clause（模拟 gorm.Statement.Build 的构建流程）
// 生成带子句名前缀的完整 SQL。
func buildClauseSQL(t *testing.T, name string, expr clause.Expression) string {
	t.Helper()
	stmt := newStatement(t)
	cc := clause.Clause{Name: name, Expression: expr}
	cc.Build(stmt)
	return stmt.SQL.String()
}
