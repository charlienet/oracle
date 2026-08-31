package oracle

import (
	"database/sql"
	"fmt"
	"hash/crc32"
	"regexp"
	"strconv"
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
	// 尝试指针类型
	if d, ok := m.Dialector.(*Dialector); ok {
		return d.DBVer
	}
	// 尝试值类型
	if d, ok := m.Dialector.(Dialector); ok {
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
	// 兼容值接收者与指针接收者两种形态：先尝试指针断言，失败则取地址
	var dialector *Dialector
	if d, ok := m.Dialector.(*Dialector); ok {
		dialector = d
	} else if d, ok := m.Dialector.(Dialector); ok {
		dialector = &d
	} else {
		return
	}
	m.DB.Raw(
		fmt.Sprintf(`SELECT ORA_DATABASE_NAME as "Current Database" FROM %s`, dialector.DummyTableName()),
	).Row().Scan(&name) //nolint:errcheck // 始终返回 "" 作为兜底，Scan 错误不影响语义
	return
}

func (m Migrator) CreateTable(values ...any) error {
	// 捕获各关系的 ON UPDATE 约束信息，供建表后创建触发器使用。
	// 必须在 TryRemoveOnUpdate 清理 TagSettings 之前解析：
	// 该清理会从 CONSTRAINT 标签中移除 OnUpdate 子项（否则建表 DDL 生成
	// Oracle 不支持的 ON UPDATE 子句，ORA-00907），但触发器逻辑依赖
	// 同一标签解析 OnUpdate，若先清理则触发器永远不会创建。
	type onUpdateInfo struct {
		value      any
		rel        *schema.Relationship
		constraint *schema.Constraint
		table      string
	}
	onUpdateInfos := make([]onUpdateInfo, 0)
	for _, value := range values {
		_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
			if stmt.Schema == nil {
				return nil
			}
			for _, rel := range stmt.Schema.Relationships.Relations {
				if c := rel.ParseConstraint(); c != nil && c.OnUpdate != "" {
					onUpdateInfos = append(onUpdateInfos, onUpdateInfo{
						value: value, rel: rel, constraint: c, table: stmt.Schema.Table,
					})
				}
			}
			return nil
		})
	}

	// 清理 CONSTRAINT 标签中的 ON UPDATE 子项，避免建表 DDL 报 ORA-00907
	for _, value := range values {
		if err := m.TryRemoveOnUpdate(value); err != nil {
			return err
		}
	}

	// 先创建表
	if err := m.Migrator.CreateTable(values...); err != nil {
		return err
	}

	// 为带 comment 标签的字段添加列注释
	// Oracle 不支持在 CREATE TABLE 中内联注释，必须使用 COMMENT ON COLUMN
	for _, value := range values {
		if err := m.addComments(value); err != nil {
			return err
		}
	}

	// 然后创建 ON UPDATE 触发器（使用清理前捕获的 constraint 信息）
	for _, info := range onUpdateInfos {
		if err := m.createOnUpdateTrigger(info.value, info.rel, info.constraint); err != nil {
			// 触发器创建失败不阻止表创建，但记录警告
			m.DB.Logger.Warn(m.DB.Statement.Context,
				"failed to create ON UPDATE trigger for %s.%s: %v",
				info.table, info.rel.Field.Name, err)
		}
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
	if len(name) > 30 {
		// 使用 CRC32 哈希保证唯一性（8 字符）
		hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(table)))
		name = name[:21] + "_" + hash // 21 + 1 + 8 = 30
	}
	return name
}

// triggerName 返回自增主键对应的触发器名
func (m Migrator) triggerName(table string) string {
	name := fmt.Sprintf("TRG_%s", table)
	if len(name) > 30 {
		hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(table)))
		name = name[:21] + "_" + hash
	}
	return name
}

// validateOracleIdentifier 验证 Oracle 标识符是否合法
func validateOracleIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	if len(name) > 30 {
		return fmt.Errorf("identifier %q exceeds 30 characters", name)
	}

	// 只允许字母、数字、下划线、$、#
	for i, r := range name {
		if i == 0 {
			// 第一个字符只能是字母或下划线
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return fmt.Errorf("identifier %q contains invalid characters", name)
			}
		} else {
			// 其他字符可以是字母、数字、下划线、$、#
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '$' && r != '#' {
				return fmt.Errorf("identifier %q contains invalid characters", name)
			}
		}
	}
	return nil
}

// addComments 为带 comment 标签的字段添加列注释
// Oracle 不支持在 CREATE TABLE 或 ALTER TABLE 中内联注释，必须使用 COMMENT ON COLUMN 语法
func (m Migrator) addComments(value any) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema == nil {
			return nil
		}

		for _, field := range stmt.Schema.Fields {
			if field.Comment != "" {
				// COMMENT ON COLUMN table.column IS 'comment'
				commentSQL := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '%s'",
					stmt.Table, field.DBName, field.Comment)
				if err := m.DB.Exec(commentSQL).Error; err != nil {
					// 注释添加失败不阻断流程，记录警告
					m.DB.Logger.Warn(stmt.Context,
						"failed to add comment on column %s.%s: %v",
						stmt.Table, field.DBName, err)
				}
			}
		}

		return nil
	})
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

			// 验证标识符安全性
			if err := validateOracleIdentifier(field.DBName); err != nil {
				return err
			}
			if err := validateOracleIdentifier(stmt.Table); err != nil {
				return err
			}

			seqName := m.sequenceName(stmt.Table)
			trgName := m.triggerName(stmt.Table)

			// 创建序列
			seqSQL := fmt.Sprintf(`BEGIN
    EXECUTE IMMEDIATE 'CREATE SEQUENCE %s START WITH 1 INCREMENT BY 1 CACHE 20';
EXCEPTION
    WHEN OTHERS THEN
        IF SQLCODE != -955 THEN RAISE; END IF;
END;`, seqName)
			if err := m.DB.Exec(seqSQL).Error; err != nil {
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
	// 验证标识符安全性
	if err := validateOracleIdentifier(field.DBName); err != nil {
		return err
	}
	if err := validateOracleIdentifier(stmt.Table); err != nil {
		return err
	}

	trgName := fmt.Sprintf("SEQDEF_TRG_%s_%s", stmt.Table, field.DBName)
	if len(trgName) > 30 {
		combined := stmt.Table + "_" + field.DBName
		hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(combined)))
		trgName = trgName[:21] + "_" + hash
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

	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
			parts := strings.SplitN(stmt.Schema.Table, ".", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("invalid table name format: %s", stmt.Schema.Table)
			}
			// 去除可能的引号
			owner := strings.Trim(parts[0], `"`)
			table := strings.Trim(parts[1], `"`)
			return m.DB.Raw("SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER = ? AND TABLE_NAME = ?",
				strings.ToUpper(owner), strings.ToUpper(table)).Row().Scan(&count)
		} else {
			return m.DB.Raw("SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = ?", stmt.Table).Row().Scan(&count)
		}
	})

	return count > 0
}

// GetTables 返回当前用户的所有表名（Oracle 数据字典 USER_TABLES）
// 覆写 GORM 默认实现（使用 information_schema.tables，Oracle 不支持）
func (m Migrator) GetTables() ([]string, error) {
	var tables []string
	err := m.DB.Raw("SELECT TABLE_NAME FROM USER_TABLES").Scan(&tables).Error
	return tables, err
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

		// 在循环前一次性查询所有列的类型
		dataTypes := make(map[string]string)
		if rows, err := m.DB.Raw("SELECT COLUMN_NAME, DATA_TYPE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ?", stmt.Table).Rows(); err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var colName, dt string
				if err := rows.Scan(&colName, &dt); err == nil {
					dataTypes[strings.ToUpper(colName)] = dt
				}
			}
		}

		// 查询所有列的注释
		comments := make(map[string]string)
		if rows, err := m.DB.Raw("SELECT COLUMN_NAME, COMMENTS FROM USER_COL_COMMENTS WHERE TABLE_NAME = ?", stmt.Table).Rows(); err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var colName string
				var comment sql.NullString
				if err := rows.Scan(&colName, &comment); err == nil && comment.Valid {
					comments[strings.ToUpper(colName)] = comment.String
				}
			}
		}

		for _, c := range rawColumnTypes {
			ct := migrator.ColumnType{SQLColumnType: c}

			// 映射列名大小写
			upperName := strings.ToUpper(c.Name())
			if dbName, ok := upperToDBName[upperName]; ok {
				ct.NameValue = sql.NullString{String: dbName, Valid: true}
			}

			if dt, ok := dataTypes[upperName]; ok && dt != "" {
				ct.DataTypeValue = sql.NullString{String: dt, Valid: true}
			}

			// 设置注释
			if comment, ok := comments[upperName]; ok {
				ct.CommentValue = sql.NullString{String: comment, Valid: true}
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
			"ALTER TABLE ? DROP COLUMN ?",
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

// MigrateColumn 迁移列定义（Oracle 使用 MODIFY 语法，而非 ALTER COLUMN）
// 覆写 GORM 默认实现，适配 Oracle 的类型系统和语法：
// 1. Oracle 使用 NUMBER 统一表示数值类型，需通过 GetTypeAliases 处理类型别名
// 2. Oracle 使用 ALTER TABLE ... MODIFY ... 而非 ALTER COLUMN ... TYPE
func (m Migrator) MigrateColumn(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	if field.IgnoreMigration {
		return nil
	}

	// 获取字段的完整数据类型定义
	fullDataType := strings.TrimSpace(strings.ToLower(m.FullDataTypeOf(field).SQL))
	realDataType := strings.ToLower(columnType.DatabaseTypeName())

	var (
		alterColumn bool
		isSameType  = fullDataType == realDataType
	)

	// 非主键字段检查类型变更
	if !field.PrimaryKey {
		// 检查类型是否匹配
		if !strings.HasPrefix(fullDataType, realDataType) {
			// Oracle 类型别名处理：NUMBER 可表示 INTEGER/SMALLINT 等
			aliases := m.GetTypeAliases(realDataType)
			for _, alias := range aliases {
				if strings.HasPrefix(fullDataType, strings.ToLower(alias)) {
					isSameType = true
					break
				}
			}

			if !isSameType {
				alterColumn = true
			}
		}
	}

	// 类型不同时，检查长度和精度
	if !isSameType {
		// 检查长度
		if length, ok := columnType.Length(); ok {
			if length != int64(field.Size) {
				if length > 0 && field.Size > 0 {
					// 双方都有长度且不等
					alterColumn = true
				} else if ok && length > 0 {
					// 数据库有长度，检查是否在字段类型定义中
					// 例如：varchar(50) vs varchar，前者有长度后者无
					// 使用正则提取字段类型中的长度
					if matches := regFullDataType.FindAllStringSubmatch(fullDataType, -1); len(matches) > 0 {
						if matches[0][1] != fmt.Sprint(length) {
							alterColumn = true
						}
					}
				}
			}
		}

		// 检查精度（decimal/numeric 类型）
		if precision, _, ok := columnType.DecimalSize(); ok && int64(field.Precision) != precision {
			// 检查字段类型定义中是否包含精度
			if regexp.MustCompile(fmt.Sprintf("[^0-9]%d[^0-9]", field.Precision)).MatchString(m.DataTypeOf(field)) {
				alterColumn = true
			}
		}
	}

	// 检查可空性变更
	if nullable, ok := columnType.Nullable(); ok && nullable == field.NotNull {
		// 数据库可空但字段非空，或反之
		// 注意：非主键且数据库可空时才修改
		if !field.PrimaryKey && nullable {
			alterColumn = true
		}
	}

	// 检查默认值变更（非主键字段）
	if !field.PrimaryKey {
		currentDefaultNotNull := field.HasDefaultValue && (field.DefaultValueInterface != nil || !strings.EqualFold(field.DefaultValue, "NULL"))
		dbDefault, dbDefaultNotNull := columnType.DefaultValue()

		// 默认值状态变更：NULL <-> NOT NULL
		if dbDefaultNotNull && !currentDefaultNotNull {
			// 数据库有默认值 -> 模型无默认值
			alterColumn = true
		} else if !dbDefaultNotNull && currentDefaultNotNull {
			// 数据库无默认值 -> 模型有默认值
			alterColumn = true
		} else if currentDefaultNotNull || dbDefaultNotNull {
			// 都有默认值，比较值是否相同
			switch field.GORMDataType {
			case schema.Time:
				// 时间类型：去掉括号后比较（CURRENT_TIMESTAMP vs CURRENT_TIMESTAMP()）
				if !strings.EqualFold(strings.TrimSuffix(dbDefault, "()"), strings.TrimSuffix(field.DefaultValue, "()")) {
					alterColumn = true
				}
			case schema.Bool:
				// 布尔类型：解析后比较
				v1, _ := strconv.ParseBool(dbDefault)
				v2, _ := strconv.ParseBool(field.DefaultValue)
				if v1 != v2 {
					alterColumn = true
				}
			default:
				// 其他类型：直接比较
				if dbDefault != field.DefaultValue {
					alterColumn = true
				}
			}
		}
	}

	// 检查注释变更
	if comment, ok := columnType.Comment(); ok && comment != field.Comment {
		if !field.PrimaryKey {
			alterColumn = true
		}
	}

	// 如果需要修改列，调用 AlterColumn（Oracle 实现使用 MODIFY 语法）
	if alterColumn {
		if err := m.AlterColumn(value, field.DBName); err != nil {
			return err
		}
	}

	// 处理注释变更：Oracle 不支持在 ALTER TABLE 中修改注释，需要单独处理
	// COMMENT ON COLUMN 语法：COMMENT ON COLUMN table.column IS 'comment'
	// Oracle 会将空字符串 '' 视为删除注释
	comment, commentOk := columnType.Comment()
	if field.Comment != "" && (!commentOk || comment != field.Comment) {
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			commentSQL := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '%s'",
				stmt.Table, field.DBName, field.Comment)
			return m.DB.Exec(commentSQL).Error
		}); err != nil {
			return err
		}
	} else if field.Comment == "" && commentOk && comment != "" {
		// 删除注释
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			commentSQL := fmt.Sprintf("COMMENT ON COLUMN %s.%s IS NULL",
				stmt.Table, field.DBName)
			return m.DB.Exec(commentSQL).Error
		}); err != nil {
			return err
		}
	}

	// 处理唯一约束变更
	return m.MigrateColumnUnique(value, field, columnType)
}

// regFullDataType 用于从数据类型字符串中提取长度/精度
var regFullDataType = regexp.MustCompile(`\D*(\d+)\D?`)

// MigrateColumnUnique 处理唯一约束变更
func (m Migrator) MigrateColumnUnique(value interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	unique, ok := columnType.Unique()
	if !ok || field.PrimaryKey {
		return nil // 跳过主键
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		// 唯一约束名称
		constraint := m.DB.NamingStrategy.UniqueName(stmt.Table, field.DBName)

		// 数据库唯一但模型非唯一：删除约束
		if unique && !field.Unique {
			return m.DropConstraint(value, constraint)
		}

		// 数据库非唯一但模型唯一：创建约束
		if !unique && field.Unique {
			return m.CreateConstraint(value, constraint)
		}

		return nil
	})
}

func (m Migrator) HasColumn(value any, field string) bool {
	var count int64
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
			parts := strings.SplitN(stmt.Schema.Table, ".", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("invalid table name format: %s", stmt.Schema.Table)
			}
			owner := strings.Trim(parts[0], `"`)
			table := strings.Trim(parts[1], `"`)
			return m.DB.Raw("SELECT COUNT(*) FROM ALL_TAB_COLUMNS WHERE OWNER = ? AND TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)",
				strings.ToUpper(owner), strings.ToUpper(table), field).Row().Scan(&count)
		} else {
			return m.DB.Raw("SELECT COUNT(*) FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", stmt.Table, field).Row().Scan(&count)
		}
	}) == nil && count > 0
}

func (m Migrator) AlterDataTypeOf(stmt *gorm.Statement, field *schema.Field) (expr clause.Expr) {
	expr.SQL = m.DataTypeOf(field)

	var nullable = ""
	if stmt.Schema != nil && strings.Contains(stmt.Schema.Table, ".") {
		parts := strings.SplitN(stmt.Schema.Table, ".", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			// 如果格式无效，跳过查询，保持 nullable 为空
		} else {
			owner := strings.Trim(parts[0], `"`)
			table := strings.Trim(parts[1], `"`)
			m.DB.Raw("SELECT NULLABLE FROM ALL_TAB_COLUMNS WHERE OWNER = ? AND TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)",
				strings.ToUpper(owner), strings.ToUpper(table), field.DBName).Row().Scan(&nullable) //nolint:errcheck // Scan 失败时 nullable 保持零值 ""，后续逻辑兼容
		}
	} else {
		m.DB.Raw("SELECT NULLABLE FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND UPPER(COLUMN_NAME) = UPPER(?)", stmt.Table, field.DBName).Row().Scan(&nullable) //nolint:errcheck // Scan 失败时 nullable 保持零值 ""，后续逻辑兼容
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
			m.BindVarTo(defaultStmt, defaultStmt, field.DefaultValueInterface)
			expr.SQL += " DEFAULT " + m.Explain(defaultStmt.SQL.String(), field.DefaultValueInterface)
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
			m.BindVarTo(defaultStmt, defaultStmt, field.DefaultValueInterface)
			expr.SQL += " DEFAULT " + m.Explain(defaultStmt.SQL.String(), field.DefaultValueInterface)
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
	_ = m.TryRemoveOnUpdate(value)
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

// constraintExistsQuery 生成 HasConstraint 的数据字典查询（纯函数，便于无库单测）。
// 表名/约束名在 Oracle 数据字典中按大写存储，传入时保持与 Namer 命名一致的大写形式。
func constraintExistsQuery(table, name string) (sql string, args []any) {
	return "SELECT COUNT(*) FROM USER_CONSTRAINTS WHERE TABLE_NAME = ? AND CONSTRAINT_NAME = ?", []any{table, name}
}

func (m Migrator) HasConstraint(value any, name string) bool {
	var count int64
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		sql, args := constraintExistsQuery(stmt.Table, name)
		return m.DB.Raw(sql, args...).Row().Scan(&count)
	}) == nil && count > 0
}

// DropView 删除视图（Oracle 不支持 DROP VIEW IF EXISTS，使用 PL/SQL 异常处理保证幂等）
// 覆写 GORM 默认实现（使用 DROP VIEW IF EXISTS，Oracle 不支持）
func (m Migrator) DropView(name string) error {
	// 使用 PL/SQL 块处理异常：ORA-00942（视图不存在）时忽略
	sql := fmt.Sprintf(`BEGIN
	EXECUTE IMMEDIATE 'DROP VIEW %s CASCADE CONSTRAINTS';
EXCEPTION
	WHEN OTHERS THEN
		IF SQLCODE != -942 THEN RAISE; END IF;
END;`, name)
	return m.DB.Exec(sql).Error
}

func (m Migrator) DropIndex(value any, name string) error {
	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}

		return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}).Error
	})
}

func (m Migrator) HasIndex(value any, name string) bool {
	var count int64
	_ = m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if idx := stmt.Schema.LookIndex(name); idx != nil {
			name = idx.Name
		}
		// 索引名已是完整名称（如 IDX_TEST_USERS_EMAIL），直接大写后与 USER_INDEXES 中存储的名称比较，
		// 不能再次通过 IndexName() 拼装，否则会得到错误的名字。
		indexName := strings.ToUpper(name)
		tableName := stmt.Table
		if strings.Contains(tableName, ".") {
			parts := strings.SplitN(tableName, ".", 2)
			tableName = parts[1]
		}
		return m.DB.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE TABLE_NAME = ? AND INDEX_NAME = ?",
			tableName,
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

// removeOnUpdateFromConstraint 从 CONSTRAINT 标签值中移除 OnUpdate 子项（大小写不敏感）。
// GORM 的 ParseTagSetting 只大写化 key、保留值原文（如 "OnUpdate:CASCADE,OnDelete:SET NULL"），
// 因此不能依赖字符串替换 "ON UPDATE xxx"（该模式永不命中），需按 "," 拆分后逐项判断：
// key（冒号前部分）与 "OnUpdate" 做 EqualFold 匹配，命中则丢弃该子项，其余子项原样保留。
func removeOnUpdateFromConstraint(constraintStr string) string {
	if constraintStr == "" {
		return ""
	}
	parts := strings.Split(constraintStr, ",")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		key := part
		if before, _, ok := strings.Cut(part, ":"); ok {
			key = before
		}
		if strings.EqualFold(strings.TrimSpace(key), "OnUpdate") {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, ",")
}

func (m Migrator) TryRemoveOnUpdate(values ...any) error {
	for _, value := range values {
		if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
			for _, rel := range stmt.Schema.Relationships.Relations {
				if str, ok := rel.Field.TagSettings["CONSTRAINT"]; ok && str != "" {
					// 原地清理 TagSettings：建表/建约束 DDL 不再生成
					// Oracle 不支持的 "ON UPDATE" 子句（ORA-00907）。
					rel.Field.TagSettings["CONSTRAINT"] = removeOnUpdateFromConstraint(str)
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
		combined := table + "_" + fkCol + "_" + refCol
		hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(combined)))
		name = name[:21] + "_" + hash
	}
	return name
}

// CreateOnUpdateTrigger 创建 ON UPDATE 触发器
// Oracle 不支持原生的 ON UPDATE 外键操作，需要通过触发器模拟
func (m Migrator) CreateOnUpdateTrigger(value any, rel *schema.Relationship) error {
	if rel == nil {
		return fmt.Errorf("relationship is nil")
	}
	return m.createOnUpdateTrigger(value, rel, rel.ParseConstraint())
}

// createOnUpdateTrigger 是 CreateOnUpdateTrigger 的内部实现。
// constraint 由调用方在 TryRemoveOnUpdate 清理 TagSettings 之前解析并显式传入，
// 避免触发器逻辑读到已被移除 OnUpdate 的 CONSTRAINT 标签而永远不会创建。
func (m Migrator) createOnUpdateTrigger(value any, rel *schema.Relationship, constraint *schema.Constraint) error {
	if rel == nil {
		return fmt.Errorf("relationship is nil")
	}

	if constraint == nil || constraint.OnUpdate == "" {
		return nil
	}

	// 只处理 CASCADE 和 SET NULL
	if constraint.OnUpdate != "CASCADE" && constraint.OnUpdate != "SET NULL" {
		return nil
	}

	// 子表外键列：belongs_to 关系下 rel.Field.DBName 为空（关联字段非物理列，
	// 外键列是子表上的 PARENT_ID 等），必须取 constraint.ForeignKeys。
	// 否则 validateOracleIdentifier 报 "identifier cannot be empty"，
	// 触发器永远不会创建（ON UPDATE 级联语义丢失）。
	var childFKCol string
	if len(constraint.ForeignKeys) > 0 {
		childFKCol = constraint.ForeignKeys[0].DBName
	} else {
		childFKCol = rel.Field.DBName // 兜底：无外键信息时退回原逻辑
	}
	if childFKCol == "" || len(constraint.References) == 0 {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		// 验证标识符安全性
		if err := validateOracleIdentifier(childFKCol); err != nil {
			return err
		}
		if err := validateOracleIdentifier(stmt.Schema.Table); err != nil {
			return err
		}
		if err := validateOracleIdentifier(constraint.References[0].DBName); err != nil {
			return err
		}
		if err := validateOracleIdentifier(constraint.ReferenceSchema.Table); err != nil {
			return err
		}

		triggerName := onUpdateTriggerName(
			stmt.Schema.Table,
			childFKCol,
			constraint.References[0].DBName,
		)

		var triggerSQL string

		switch constraint.OnUpdate {
		case "CASCADE":
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
				childFKCol,
				constraint.References[0].DBName,
				childFKCol,
				constraint.References[0].DBName,
			)
		case "SET NULL":
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
				childFKCol,
				childFKCol,
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

	// ParseConstraint 需要完整的 schema 关系信息（FieldSchema 等），
	// 如果关系不完整（如测试中手动构造的 rel），直接跳过
	if rel.FieldSchema == nil {
		return nil
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		constraint := rel.ParseConstraint()
		if constraint == nil || len(constraint.References) == 0 {
			return nil
		}
		triggerName := onUpdateTriggerName(
			stmt.Schema.Table,
			rel.Field.DBName,
			constraint.References[0].DBName,
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

// GetTypeAliases 返回字段逻辑类型在 Oracle 数据字典中的别名，用于 MigrateColumn
// 的类型比对：Oracle 把 INTEGER/SMALLINT 等数值列统一报告为 NUMBER，
// 覆写默认实现避免 AutoMigrate 反复误判为类型差异而重复 ALTER。
//
// MigrateColumn 匹配逻辑（gorm@v1.31.2/migrator/migrator.go:484-493）：
//
//	if !strings.HasPrefix(fullDataType, realDataType) {
//	    aliases := m.DB.Migrator().GetTypeAliases(realDataType)
//	    for _, alias := range aliases {
//	        if strings.HasPrefix(fullDataType, alias) { isSameType = true }
//	    }
//	}
//
// 因此 aliases 应是 DataTypeOf 输出值的前缀（如 "integer"、"smallint"），
// 而非 Oracle 侧的类型名 "number"。
func (m Migrator) GetTypeAliases(databaseTypeName string) []string {
	switch strings.ToLower(databaseTypeName) {
	case "number":
		return []string{"integer", "smallint"}
	default:
		return nil
	}
}

// oracleIndex 实现 gorm.Index 接口，用于 GetIndexes 方法
type oracleIndex struct {
	name    string
	table   string
	columns []string
	unique  bool
	primary bool
}

func (i *oracleIndex) Table() string            { return i.table }
func (i *oracleIndex) Name() string             { return i.name }
func (i *oracleIndex) Columns() []string        { return i.columns }
func (i *oracleIndex) PrimaryKey() (bool, bool) { return i.primary, i.primary }
func (i *oracleIndex) Unique() (bool, bool)     { return i.unique, i.unique }
func (i *oracleIndex) Option() string           { return "" }

// oracleTableType 实现 gorm.TableType 接口，用于 TableType 方法
type oracleTableType struct {
	schema  string
	name    string
	typ     string
	comment string
}

func (t *oracleTableType) Schema() string          { return t.schema }
func (t *oracleTableType) Name() string            { return t.name }
func (t *oracleTableType) Type() string            { return t.typ }
func (t *oracleTableType) Comment() (string, bool) { return t.comment, t.comment != "" }

// primaryIndexQuery 生成 GetIndexes 中主键索引判定的数据字典查询（纯函数，便于无库单测）。
func primaryIndexQuery(indexName string) (sql string, args []any) {
	return "SELECT COUNT(*) FROM USER_CONSTRAINTS WHERE CONSTRAINT_TYPE = 'P' AND INDEX_NAME = ?", []any{indexName}
}

// GetIndexes 返回表的所有索引（Oracle 数据字典 USER_INDEXES）
func (m Migrator) GetIndexes(value interface{}) ([]gorm.Index, error) {
	var indexes []gorm.Index

	err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
		// 查询索引名称
		type indexInfo struct {
			IndexName  string
			TableName  string
			Uniqueness string
		}
		var idxInfos []indexInfo
		if err := m.DB.Raw(`
			SELECT INDEX_NAME, TABLE_NAME, UNIQUENESS 
			FROM USER_INDEXES 
			WHERE TABLE_NAME = ?
		`, stmt.Table).Scan(&idxInfos).Error; err != nil {
			return err
		}

		// 查询每个索引的列
		for _, idxInfo := range idxInfos {
			var columns []string
			if err := m.DB.Raw(`
				SELECT COLUMN_NAME 
				FROM USER_IND_COLUMNS 
				WHERE INDEX_NAME = ? 
				ORDER BY COLUMN_POSITION
			`, idxInfo.IndexName).Scan(&columns).Error; err != nil {
				return err
			}

			// 判断是否为主键索引
			var isPrimary bool
			var count int64
			sql, args := primaryIndexQuery(idxInfo.IndexName)
			if err := m.DB.Raw(sql, args...).Row().Scan(&count); err != nil {
				return err
			}
			isPrimary = count > 0

			indexes = append(indexes, &oracleIndex{
				name:    idxInfo.IndexName,
				table:   idxInfo.TableName,
				columns: columns,
				unique:  idxInfo.Uniqueness == "UNIQUE",
				primary: isPrimary,
			})
		}

		return nil
	})

	return indexes, err
}

// TableType 返回表类型（TABLE/VIEW 等）
func (m Migrator) TableType(value interface{}) (gorm.TableType, error) {
	var result *oracleTableType

	err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
		// 先查询是否为表
		var tableCount int64
		if err := m.DB.Raw("SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = ?", stmt.Table).Row().Scan(&tableCount); err != nil {
			return err
		}

		if tableCount > 0 {
			result = &oracleTableType{
				name: stmt.Table,
				typ:  "TABLE",
			}
			return nil
		}

		// 再查询是否为视图
		var viewCount int64
		if err := m.DB.Raw("SELECT COUNT(*) FROM USER_VIEWS WHERE VIEW_NAME = ?", stmt.Table).Row().Scan(&viewCount); err != nil {
			return err
		}

		if viewCount > 0 {
			result = &oracleTableType{
				name: stmt.Table,
				typ:  "VIEW",
			}
			return nil
		}

		return fmt.Errorf("table or view %q not found", stmt.Table)
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// RenameColumn 重命名列（12c+ 支持，11g 不支持）
func (m Migrator) RenameColumn(value interface{}, oldName, newName string) error {
	// 检测数据库版本
	if !supportsIdentity(m.oracleDBVer()) {
		return fmt.Errorf("oracle 11g 不支持 RENAME COLUMN，请使用 12c+ 版本")
	}

	return m.RunWithValue(value, func(stmt *gorm.Statement) error {
		if field := stmt.Schema.LookUpField(oldName); field != nil {
			oldName = field.DBName
		}
		if field := stmt.Schema.LookUpField(newName); field != nil {
			newName = field.DBName
		}

		return m.DB.Exec(
			"ALTER TABLE ? RENAME COLUMN ? TO ?",
			clause.Table{Name: stmt.Schema.Table},
			clause.Column{Name: oldName},
			clause.Column{Name: newName},
		).Error
	})
}
