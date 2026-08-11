package oracle

import (
	"github.com/emirpasic/gods/sets/hashset"
)

var ReservedWords = hashset.New(MapStringToInterface(ReservedWordsList)...)

func IsReservedWord(v string) bool {
	return ReservedWords.Contains(v)
}

var ReservedWordsList = []string{
	"ACCESS", "ADD", "AGGREGATE", "AGGREGATES", "ALL", "ALLOW", "ALTER", "ANALYZE", "ANCESTOR", "AND", "ANY", "AS", "ASC", "AT", "AUDIT", "AVG", "BETWEEN",
	"BINARY_DOUBLE", "BINARY_FLOAT", "BLOB", "BRANCH", "BUILD", "BY", "BYTE", "CASE", "CAST", "CHAR", "CHECK", "CHILD", "CLEAR", "CLUSTER",
	"CLOB", "COMMENT", "COMMIT", "COMPILE", "CONNECT", "CONSIDER", "CONSTRAINT", "COUNT", "CREATE", "CURRENT", "DATATYPE", "DATE", "DATE_MEASURE", "DAY", "DECIMAL", "DELETE",
	"DESC", "DESCENDANT", "DIMENSION", "DISALLOW", "DISTINCT", "DIVISION", "DML", "DROP", "ELSE", "END", "ESCAPE", "EXCLUSIVE", "EXECUTE", "EXISTS", "EXPLAIN", "FIRST",
	"FLOAT", "FOR", "FROM", "GRANT", "GROUP", "HAVING", "HIERARCHIES", "HIERARCHY", "HOUR", "IDENTIFIED", "IGNORE", "IN", "INDEX", "INDICATOR", "INFINITE", "INSERT", "INTEGER",
	"INTERSECT", "INTERVAL", "INTO", "IS", "LAST", "LEAF_DESCENDANT", "LEAVES", "LEVEL", "LIKE", "LIKEC", "LIKE2", "LIKE4", "LOAD",
	"LOCAL", "LOCK", "LOG_SPEC", "LONG", "MAINTAIN", "MAX", "MEASURE", "MEASURES", "MEMBER", "MEMBERS", "MERGE", "MINUS", "MINUTE", "MLSLABEL",
	"MOD", "MODE", "MODEL", "MODIFY", "MONTH", "NAN", "NCHAR", "NCLOB", "NO", "NONE", "NOT", "NOWAIT", "NULL", "NULLS", "NUMBER",
	"NVARCHAR2", "OF", "OLAP", "OLAP_DML_EXPRESSION", "ON", "ONLY", "OPERATOR", "OPTION", "OR", "ORDER", "OVER", "OVERFLOW",
	"PARALLEL", "PARENT", "PARTITION", "PCTFREE", "PLSQL", "PRIOR", "PRIVILEGES", "PRUNE", "PUBLIC", "RAW", "RELATIVE", "RENAME", "RESOURCE", "REVOKE", "ROOT_ANCESTOR", "ROW", "ROWID", "ROWNUM", "ROWS", "SCN", "SECOND", "SELECT", "SELF",
	"SERIAL", "SESSION", "SET", "SHARE", "SIZE", "SOLVE", "SOME", "SORT", "SPEC", "START", "SUCCESSFUL", "SUM", "SYNCH", "SYNONYM", "SYSDATE", "SYSTIMESTAMP", "TABLE", "TEXT_MEASURE", "THEN", "TIME", "TIMESTAMP", "TITLE", "TO", "TRIGGER", "TYPE", "UID", "UNBRANCH", "UNION", "UNIQUE", "UNLIMITED", "UPDATE", "USER", "USING", "VALIDATE", "VALUES", "VARCHAR2", "VIEW", "WHEN", "WHERE", "WITHIN", "WITH", "YEAR",
	"ZERO", "ZONE",
}

// 辅助函数：将字符串切片转换为接口切片
func MapStringToInterface(slice []string) []interface{} {
	result := make([]interface{}, len(slice))
	for i, v := range slice {
		result[i] = v
	}
	return result
}
