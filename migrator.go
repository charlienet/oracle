package oracle

import (
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm/schema"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
)

type Migrator struct {
	migrator.Migrator
}

// oracleDBVer 返回当前数据库版本号（用于版本感知的默认值处理）。
// m.Dialector 是 gorm.Dialector 接口，需断言为 oracle.Dialector 获取 DBVer。
func (m Migrator) oracleDBVer() string {
	if d, ok := m.Dialector.(*Dialector); ok {
		return d.DBVer
	}
	return ""
}

// hasNEXTVALDefault 判断字段默认值是否为序列引用（.NEXTVAL）。
// 仅检查 DefaultValue 字符串，因为 string 类型字段的 DefaultValueInterface
// 会被 GORM 解析为字符串字面量，无法区分普通字符串与序列引用。
func hasNEXTVALDefault(field *schema.Field) bool {
	return field != nil && strings.Contains(strings.ToUpper(field.DefaultValue), ".NEXTVAL")
}

func (m Migrator) CurrentDatabase() (name string) {
	m.DB.Raw(
		fmt.Sprintf(`SELECT ORA_DATABASE_NAME as "Current Database" FROM %s`, m.Dialector.(*Dialector).DummyTableName()),
	).Row().Scan(&name)
	return
}

func (m Migrator) CreateTable(values ...any) error {
	for _, value := range values {
		m.TryRemoveOnUpdate(value)
	}

	// 先创建表
	if err := m.Migrator.CreateTable(values...); err != nil {
		return err
	}

	// 然后创建 ON UPDATE 触发器
	for _, value := range values {
		m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema == nil {
				return nil
			}
			for _, rel := range stmt.Schema.Relationships.Relations {
				if err := m.CreateOnUpdateTrigger(value, rel); err != nil {
					// 触发器创建失败不阻止表创建，只记录警告
					// 可以选择忽略或记录日志
				}
			}
			return nil
		})
	}

	// Oracle 11g 不支持 IDENTITY 列，为自增主键创建序列 + BEFORE INSERT 触发器
	for _, value := range values {
		if err := m.createAutoIncrementSupport(value); err != nil {
			return err
		}
	}

	// Oracle 11g 下使用序列默认值（DEFAULT <seq>.NEXTVAL）的字段：
	// 11g 的 DEFAULT 子句不允许引用序列 NEXTVAL（ORA-00984，12c 才支持），
	// 因此建表后为这类字段创建 BEFORE INSERT 触发器实现等价语义；12c+ 无需。
	dbVer := m.oracleDBVer()
	if !supportsIdentity(dbVer) {
		for _, value := range values {
			if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
				if stmt.Schema == nil {
					return nil
				}
				for _, field := range stmt.Schema.Fields {
					// 仅处理显式声明了序列默认值且非自增的字段。
					// 自增主键的序列逻辑由 createAutoIncrementSupport 负责，
					// 跳过以避免生成重复/冲突的 BEFORE INSERT 触发器。
					if !field.HasDefaultValue || field.AutoIncrement || !hasNEXTVALDefault(field) {
						continue
					}
					// 从 DefaultValue 提取序列名：取 ".NEXTVAL" 前的部分
					seqName := extractSequenceNameFromDefault(field.DefaultValue)
					if seqName == "" {
						continue
					}
					if err := m.createSequenceDefaultTrigger(stmt, field, seqName); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// sequenceName 返回自增主键对应的序列名
func (m Migrator) sequenceName(table string) string {
	name := fmt.Sprintf("SEQ_%s", table)
	// Oracle 标识符最多 30 字符，超长时截断避免 ORA-00972
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}

// triggerName 返回自增主键对应的触发器名
func (m Migrator) triggerName(table string) string {
	name := fmt.Sprintf("TRG_%s", table)
	// Oracle 标识符最多 30 字符，超长时截断避免 ORA-00972
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}

// createAutoIncrementSupport 为自增主键创建序列和 BEFORE INSERT 触发器（仅不支持 IDENTITY 的版本）
func (m Migrator) createAutoIncrementSupport(value any) error {
	// 12c+ 原生支持 IDENTITY 列，无需序列 + 触发器模拟
	if d, ok := m.Dialector.(*Dialector); ok && supportsIdentity(d.DBVer) {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return nil
		}

		for _, field := range stmt.Schema.Fields {
			if !field.AutoIncrement {
				continue
			}
			if field.DataType != schema.Int && field.DataType != schema.Uint {
				continue
			}

			seqName := m.sequenceName(stmt.Table)
			trgName := m.triggerName(stmt.Table)

			// 创建序列
			if err := m.DB.Exec(fmt.Sprintf("CREATE SEQUENCE %s START WITH 1 INCREMENT BY 1 NOCACHE", seqName)).Error; err != nil {
				return err
			}

			// 创建 BEFORE INSERT 触发器：ID 为空时从序列取值
			triggerSQL := fmt.Sprintf(`CREATE OR REPLACE TRIGGER %s
BEFORE INSERT ON %s
FOR EACH ROW
BEGIN
	IF :NEW.%s IS NULL THEN
		SELECT %s.NEXTVAL INTO :NEW.%s FROM DUAL;
	END IF;
END;`, trgName, stmt.Table, field.DBName, seqName, field.DBName)

			if err := m.DB.Exec(triggerSQL).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// extractSequenceNameFromDefault 从序列默认值字符串中提取序列名。
// 例如 "SEQ_MY.NEXTVAL" → "SEQ_MY"；同时兼容 GORM 对含括号默认值保持原文的
// 情况（如 "(SEQ_MY.NEXTVAL)"）。
func extractSequenceNameFromDefault(defaultValue string) string {
	v := strings.TrimSpace(defaultValue)
	// 去掉可能的包裹括号
	v = strings.TrimPrefix(v, "(")
	v = strings.TrimSuffix(v, ")")
	v = strings.TrimSpace(v)
	idx := strings.Index(strings.ToUpper(v), ".NEXTVAL")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(v[:idx])
}

// createSequenceDefaultTrigger 为 11g 下使用序列默认值的字段创建 BEFORE INSERT 触发器。
// 11g 的 DEFAULT 子句不允许引用序列 NEXTVAL（ORA-00984，12c 才支持），
// 因此在建表后通过触发器实现等价语义：插入时列值为 NULL 则从序列取值回填。
// 触发器命名为 SEQDEF_TRG_<table>_<column>，避免与 autoIncrement 的 TRG_<table> 冲突。
func (m Migrator) createSequenceDefaultTrigger(stmt *gorm.Statement, field *schema.Field, seqName string) error {
	trgName := fmt.Sprintf("SEQDEF_TRG_%s_%s", stmt.Table, field.DBName)
	// Oracle 标识符最多 30 字符，超长时截断避免 ORA-00972
	if len(trgName) > 30 {
		trgName = trgName[:30]
	}

	triggerSQL := fmt.Sprintf(`CREATE OR REPLACE TRIGGER %s
BEFORE INSERT ON %s
FOR EACH ROW
BEGIN
	IF :NEW.%s IS NULL THEN
		SELECT %s.NEXTVAL INTO :NEW.%s FROM DUAL;
	END IF;
END;`, trgName, stmt.Table, field.DBName, seqName, field.DBName)

	return m.DB.Exec(triggerSQL).Error
}

// dropSequence 删除表对应的自增序列（不存在时忽略）
func (m Migrator) dropSequence(table string) error {
	seqName := m.sequenceName(table)
	var count int64
	if err := m.DB.Raw("SELECT COUNT(*) FROM USER_SEQUENCES WHERE SEQUENCE_NAME = ?", seqName).Row().Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return m.DB.Exec("DROP SEQUENCE " + seqName).Error
	}
	return nil
}

func (m Migrator) DropTable(values ...any) error {
	values = m.ReorderModels(values, false)
	for i := len(values) - 1; i >= 0; i-- {
		value := values[i]
		tx := m.DB.Session(&gorm.Session{})
		if m.HasTable(value) {
			if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
				// PURGE：避免表进入回收站，防止同名重建时报 ORA-00955
				if err := tx.Exec("DROP TABLE ? CASCADE CONSTRAINTS PURGE", clause.Table{Name: stmt.Table}).Error; err != nil {
					return err
				}
				// 删除自增序列
				return m.dropSequence(stmt.Table)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m Migrator) HasTable(value any) bool {
	var count int64

	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
			ownertable := strings.Split(stmt.Schema.Table, ".")
			return m.DB.Raw("SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER = ?  and  TABLE_NAME = ?", ownertable[0], ownertable[1]).Row().Scan(&count)
		} else {
			return m.DB.Raw("SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = ?", stmt.Table).Row().Scan(&count)
		}
	})

	return count > 0
}

// ColumnTypes return columnTypes []gorm.ColumnType and execErr error
func (m Migrator) ColumnTypes(value any) ([]gorm.ColumnType, error) {
	columnTypes := make([]gorm.ColumnType, 0)
	execErr := m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
		rows, err := m.DB.Session(&gorm.Session{}).Table(stmt.Schema.Table).Where("ROWNUM = 1").Rows()
		if err != nil {
			return err
		}

		defer func() {
			// Close 错误不应覆盖已成功收集的返回值
			_ = rows.Close()
		}()

		var rawColumnTypes []*sql.ColumnType
		rawColumnTypes, err = rows.ColumnTypes()
		if err != nil {
			return err
		}

		// Oracle 返回大写列名，而模型字段 DBName 可能是小写（如 column:id）。
		// 将列名映射回模型定义的 DBName 大小写，避免 AutoMigrate 误判列不存在。
		upperToDBName := make(map[string]string, len(stmt.Schema.Fields))
		for _, field := range stmt.Schema.Fields {
			if field.DBName != "" {
				upperToDBName[strings.ToUpper(field.DBName)] = field.DBName
			}
		}

		for _, c := range rawColumnTypes {
			ct := migrator.ColumnType{SQLColumnType: c}

			// 映射列名大小写
			upperName := strings.ToUpper(c.Name())
			if dbName, ok := upperToDBName[upperName]; ok {
				ct.NameValue = sql.NullString{String: dbName, Valid: true}
			}

			// go-ora 未实现 RowsColumnTypeDatabaseTypeName，从数据字典获取真实数据类型，
			// 避免 AutoMigrate 对每个非主键列都误判类型变化并触发 ALTER。
			var dataType string
			if err := m.DB.Raw(
				"SELECT DATA_TYPE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND UPPER(COLUMN_NAME) = ?",
				stmt.Table, upperName,
			).Row().Scan(&dataType); err == nil && dataType != "" {
				ct.DataTypeValue = sql.NullString{String: dataType, Valid: true}
			}

			columnTypes = append(columnTypes, ct)
		}

		return
	})

	return columnTypes, execErr
}

func (m Migrator) RenameTable(oldName, newName any) (err error) {
	resolveTable := func(name any) (result string, err error) {
		if v, ok := name.(string); ok {
			result = v
		} else {
			stmt := &gorm.Statement{DB: m.DB}
			if err = stmt.Parse(name); err == nil {
				result = stmt.Table
			}
		}
		return
	}

	var oldTable, newTable string

	if oldTable, err = resolveTable(oldName); err != nil {
		return
	}

	if newTable, err = resolveTable(newName); err != nil {
		return
	}

	if !m.HasTable(oldTable) {
		return
	}

	return m.DB.Exec("RENAME ? TO ?",
		clause.Table{Name: oldTable},
		clause.Table{Name: newTable},
	).Error
}

func (m Migrator) AddColumn(value any, field string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(field); field != nil {
			return m.DB.Exec(
				"ALTER TABLE ? ADD ? ?",
				clause.Table{Name: stmt.Schema.Table}, clause.Column{Name: field.DBName}, m.DB.Migrator().FullDataTypeOf(field),
			).Error
		}
		return fmt.Errorf("failed to look up field with name: %s", field)
	})
}

func (m Migrator) DropColumn(value any, name string) error {
	if !m.HasColumn(value, name) {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(name); field != nil {
			name = field.DBName
		}

		return m.DB.Exec(
			"ALTER TABLE ? DROP ?",
			clause.Table{Name: stmt.Schema.Table},
			clause.Column{Name: name},
		).Error
	})
}

func (m Migrator) AlterColumn(value any, field string) error {
	if !m.HasColumn(value, field) {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(field); field != nil {
			return m.DB.Exec(
				"ALTER TABLE ? MODIFY ? ?",
				clause.Table{Name: stmt.Schema.Table},
				clause.Column{Name: field.DBName},
				m.AlterDataTypeOf(stmt, field),
			).Error
		}
		return fmt.Errorf("failed to look up field with name: %s", field)
	})
}

func (m Migrator) HasColumn(value any, field string) bool {
	var count int64
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
			ownertable := strings.Split(stmt.Schema.Table, ".")
			return m.DB.Raw("SELECT COUNT(*) FROM ALL_TAB_COLUMNS WHERE OWNER = ? AND TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", ownertable[0], ownertable[1], field).Row().Scan(&count)
		} else {
			return m.DB.Raw("SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", stmt.Table, field).Row().Scan(&count)
		}
	}) == nil && count > 0
}

func (m Migrator) AlterDataTypeOf(stmt *gorm.Statement, field *schema.Field) (expr clause.Expr) {
	expr.SQL = m.DataTypeOf(field)

	var nullable = ""
	if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
		ownertable := strings.Split(stmt.Schema.Table, ".")
		m.DB.Raw("SELECT NULLABLE FROM ALL_TAB_COLUMNS WHERE OWNER = ? AND TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", ownertable[0], ownertable[1], field.DBName).Row().Scan(&nullable)
	} else {
		m.DB.Raw("SELECT NULLABLE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", stmt.Table, field.DBName).Row().Scan(&nullable)
	}
	if field.NotNull && nullable == "Y" {
		expr.SQL += " NOT NULL"
	}

	if field.Unique {
		expr.SQL += " UNIQUE"
	}

	if field.HasDefaultValue && (field.DefaultValueInterface != nil || field.DefaultValue != "") {
		// 序列默认值（.NEXTVAL）优先走版本感知处理：
		// string 字段的 DefaultValueInterface 会被 GORM 解析为字符串字面量（如 "SEQ_X.NEXTVAL"），
		// 直接拼接会生成错误的字符串默认值而非序列引用，必须先识别出来。
		if hasNEXTVALDefault(field) {
			if dv := buildOracleDefault(m.oracleDBVer(), field.DefaultValue, field); dv != "" {
				expr.SQL += " " + dv
			}
			return
		}

		if field.DefaultValueInterface != nil {
			defaultStmt := &gorm.Statement{Vars: []any{field.DefaultValueInterface}}
			m.Dialector.BindVarTo(defaultStmt, defaultStmt, field.DefaultValueInterface)
			expr.SQL += " DEFAULT " + m.Dialector.Explain(defaultStmt.SQL.String(), field.DefaultValueInterface)
		} else if field.DefaultValue != "(-)" {
			// 使用 buildOracleDefault 进行智能转换（版本感知：11g 下 NEXTVAL 默认值
			// 不能生成 DEFAULT 子句，返回空串则跳过，避免拼出非法 SQL）
			if dv := buildOracleDefault(m.oracleDBVer(), field.DefaultValue, field); dv != "" {
				expr.SQL += " " + dv
			}
		}
	}

	return
}

// FullDataTypeOf 返回字段的完整数据库类型（版本感知的默认值处理）。
// GORM 标准实现会把 DefaultValue 直接拼成 "DEFAULT xxx"，对 11g 下引用序列的
// NEXTVAL 默认值会生成非法 SQL（ORA-00984），因此在此重写：
// 11g 下 NEXTVAL 默认值不生成 DEFAULT 子句，改由 CreateTable 流程在建表后
// 创建 BEFORE INSERT 触发器实现等价语义（见 createSequenceDefaultTrigger）。
func (m Migrator) FullDataTypeOf(field *schema.Field) (expr clause.Expr) {
	expr.SQL = m.DataTypeOf(field)

	if field.NotNull {
		expr.SQL += " NOT NULL"
	}

	if field.HasDefaultValue && (field.DefaultValueInterface != nil || field.DefaultValue != "") {
		// 序列默认值（.NEXTVAL）优先走版本感知处理：
		// string 字段的 DefaultValueInterface 会被 GORM 解析为字符串字面量（如 "SEQ_X.NEXTVAL"），
		// 直接拼接会生成错误的字符串默认值而非序列引用，必须先识别出来。
		if hasNEXTVALDefault(field) {
			dbVer := m.oracleDBVer()
			if dv := buildOracleDefault(dbVer, field.DefaultValue, field); dv != "" {
				expr.SQL += " " + dv
			}
			return
		}

		if field.DefaultValueInterface != nil {
			defaultStmt := &gorm.Statement{Vars: []any{field.DefaultValueInterface}}
			m.Dialector.BindVarTo(defaultStmt, defaultStmt, field.DefaultValueInterface)
			expr.SQL += " DEFAULT " + m.Dialector.Explain(defaultStmt.SQL.String(), field.DefaultValueInterface)
		} else if field.DefaultValue != "(-)" {
			// 版本感知：11g 下 NEXTVAL 默认值不能用 DEFAULT 子句（此处非 NEXTVAL
			// 场景由 buildOracleDefault 正常生成 DEFAULT 子句）
			if dv := buildOracleDefault(m.oracleDBVer(), field.DefaultValue, field); dv != "" {
				expr.SQL += " " + dv
			}
		}
	}

	return
}

func (m Migrator) CreateConstraint(value any, name string) error {
	m.TryRemoveOnUpdate(value)
	return m.Migrator.CreateConstraint(value, name)
}

func (m Migrator) DropConstraint(value any, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		for _, chk := range stmt.Schema.ParseCheckConstraints() {
			if chk.Name == name {
				return m.DB.Exec(
					"ALTER TABLE ? DROP CHECK ?",
					clause.Table{Name: stmt.Schema.Table}, clause.Column{Name: name},
				).Error
			}
		}

		return m.DB.Exec(
			"ALTER TABLE ? DROP CONSTRAINT ?",
			clause.Table{Name: stmt.Schema.Table}, clause.Column{Name: name},
		).Error
	})
}

func (m Migrator) HasConstraint(value any, name string) bool {
	var count int64
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Raw(
			"SELECT COUNT(*) FROM USER_CONSTRAINTS WHERE TABLE_NAME = ? AND CONSTRAINT_NAME = ?", stmt.Table, name,
		).Row().Scan(&count)
	}) == nil && count > 0
}

func (m Migrator) DropIndex(value any, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}, clause.Table{Name: stmt.Schema.Table}).Error
	})
}

func (m Migrator) HasIndex(value any, name string) bool {
	var count int64
	m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}
		// 索引名已是完整名称（如 IDX_TEST_USERS_EMAIL），直接大写后与 USER_INDEXES 中存储的名称比较，
		// 不能再次通过 IndexName() 拼装，否则会得到错误的名字。
		indexName := strings.ToUpper(name)
		return m.DB.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE TABLE_NAME = ? AND INDEX_NAME = ?",
			m.Migrator.DB.NamingStrategy.TableName(stmt.Table),
			indexName,
		).Row().Scan(&count)
	})

	return count > 0
}

// https://docs.oracle.com/database/121/SPATL/alter-index-rename.htm
func (m Migrator) RenameIndex(value any, oldName, newName string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		return m.DB.Exec(
			"ALTER INDEX ? RENAME TO ?", // wat
			clause.Column{Name: oldName}, clause.Column{Name: newName},
		).Error
	})
}

func (m Migrator) TryRemoveOnUpdate(values ...any) error {
	for _, value := range values {
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			for _, rel := range stmt.Schema.Relationships.Relations {
				constraint := rel.ParseConstraint()
				if constraint != nil {
					rel.Field.TagSettings["CONSTRAINT"] = strings.ReplaceAll(rel.Field.TagSettings["CONSTRAINT"], fmt.Sprintf("ON UPDATE %s", constraint.OnUpdate), "")
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// onUpdateTriggerName 生成 ON UPDATE 触发器名。
// Oracle 标识符最多 30 字符，超长时截断避免 ORA-00972。
func onUpdateTriggerName(table, fkCol, refCol string) string {
	name := fmt.Sprintf("fk_trigger_%s_%s_%s", table, fkCol, refCol)
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}

// CreateOnUpdateTrigger 创建 ON UPDATE 触发器
// Oracle 不支持原生的 ON UPDATE 外键操作，需要通过触发器模拟
func (m Migrator) CreateOnUpdateTrigger(value any, rel *schema.Relationship) error {
	if rel == nil {
		return fmt.Errorf("relationship is nil")
	}

	constraint := rel.ParseConstraint()
	if constraint == nil || constraint.OnUpdate == "" {
		return nil
	}

	// 只处理 CASCADE 和 SET NULL
	if constraint.OnUpdate != "CASCADE" && constraint.OnUpdate != "SET NULL" {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		triggerName := onUpdateTriggerName(
			stmt.Schema.Table,
			rel.Field.DBName,
			constraint.References[0].DBName,
		)

		var triggerSQL string

		if constraint.OnUpdate == "CASCADE" {
			// CASCADE: 当父表更新时，子表相应字段也更新
			triggerSQL = fmt.Sprintf(`
                CREATE OR REPLACE TRIGGER %s
                AFTER UPDATE OF %s ON %s
                FOR EACH ROW
                BEGIN
                    UPDATE %s SET %s = :NEW.%s WHERE %s = :OLD.%s;
                END;`,
				triggerName,
				constraint.References[0].DBName,
				constraint.ReferenceSchema.Table,
				stmt.Schema.Table,
				rel.Field.DBName,
				constraint.References[0].DBName,
				rel.Field.DBName,
				constraint.References[0].DBName,
			)
		} else if constraint.OnUpdate == "SET NULL" {
			// SET NULL: 当父表更新时，子表相应字段设为 NULL
			triggerSQL = fmt.Sprintf(`
                CREATE OR REPLACE TRIGGER %s
                AFTER UPDATE OF %s ON %s
                FOR EACH ROW
                BEGIN
                    UPDATE %s SET %s = NULL WHERE %s = :OLD.%s;
                END;`,
				triggerName,
				constraint.References[0].DBName,
				constraint.ReferenceSchema.Table,
				stmt.Schema.Table,
				rel.Field.DBName,
				rel.Field.DBName,
				constraint.References[0].DBName,
			)
		}

		if triggerSQL != "" {
			return m.DB.Exec(triggerSQL).Error
		}

		return nil
	})
}

// DropOnUpdateTrigger 删除 ON UPDATE 触发器
func (m Migrator) DropOnUpdateTrigger(value any, rel *schema.Relationship) error {
	if rel == nil {
		return fmt.Errorf("relationship is nil")
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		triggerName := onUpdateTriggerName(
			stmt.Schema.Table,
			rel.Field.DBName,
			rel.Field.DBName,
		)

		// Oracle 不支持 DROP TRIGGER IF EXISTS（MySQL/PostgreSQL 语法）。
		// 用 PL/SQL 块先检查触发器是否存在，避免 ORA-04080（触发器不存在）导致删表流程中断。
		return m.DB.Exec(fmt.Sprintf(`BEGIN
	EXECUTE IMMEDIATE 'DROP TRIGGER %s';
EXCEPTION
	WHEN OTHERS THEN
		IF SQLCODE != -4080 THEN RAISE; END IF;
END;`, triggerName)).Error
	})
}
