package oracle

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/schema"
)

// schema 补键的并发保护与已补键标记。
// gorm 的 schema 是全局缓存且解析后只读，这里在首次查询时一次性给
// FieldsByDBName 补充大写列名键，之后所有查询只读，无数据竞争。
var (
	schemaPatchMu   sync.Mutex
	schemaPatched   sync.Map // map[*schema.Schema]bool
)

// patchUpperDBNameKeys 为 schema 的 FieldsByDBName 补充大写列名键。
//
// 问题背景：Oracle 返回大写列名（如 FIRST_NAME），而模型若用显式
// `gorm:"column:first_name"`（小写）tag，字段 DBName 是小写。gorm 查询
// scan 时用 schema.LookUpField(column) 按大写列名匹配字段，找不到小写
// DBName 的字段，导致该列不回填（虚拟列等显式小写 tag 的字段全部为空）。
// 这里补充 大写DBName -> 字段 的别名键，使 LookUpField 能匹配。
func patchUpperDBNameKeys(s *schema.Schema) {
	if s == nil {
		return
	}
	
	// 检查是否已 patch
	if _, loaded := schemaPatched.Load(s); loaded {
		return
	}
	
	schemaPatchMu.Lock()
	defer schemaPatchMu.Unlock()
	
	// 双重检查
	if _, loaded := schemaPatched.Load(s); loaded {
		return
	}
	
	// 构建新 map
	newMap := make(map[string]*schema.Field, len(s.FieldsByDBName)+len(s.Fields))
	for k, v := range s.FieldsByDBName {
		newMap[k] = v
	}
	for _, field := range s.Fields {
		upper := strings.ToUpper(field.DBName)
		if upper != field.DBName {
			if _, exists := newMap[upper]; !exists {
				newMap[upper] = field
			}
		}
	}
	
	// 原子替换引用
	s.FieldsByDBName = newMap
	
	// 标记为已 patch
	schemaPatched.Store(s, true)
}

// Query 是 Oracle 特定的查询回调函数
// 处理查询前后的数据转换和列名映射
func Query(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil {
		return
	}

	// 1. 查询前处理
	preprocessQuery(db)

	// 2. 补充大写列名键，保证 gorm scan 能匹配显式小写 column tag 的字段
	patchUpperDBNameKeys(stmt.Schema)

	// 3. 执行查询（调用默认回调）
	callbacks.Query(db)

	// 4. 查询后处理
	if db.Error == nil {
		postprocessQuery(db)
	}
}

// preprocessQuery 处理查询前的预处理工作
func preprocessQuery(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil {
		return
	}

	// 预留扩展点：当前查询无需额外预处理，
	// LIMIT/OFFSET 重写已在 ClauseBuilders 中处理。
	// 后续如需 hint/优化可在本函数中添加。
}

// postprocessQuery 处理查询后的结果转换
func postprocessQuery(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil || stmt.Schema == nil {
		return
	}

	// 处理查询结果
	dest := stmt.Dest
	if dest == nil {
		return
	}

	// 获取反射值
	rv := reflect.ValueOf(dest)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	// 构建一次列名到字段的映射（Oracle 返回大写列名），避免每行重复构建
	columnToField := make(map[string]*schema.Field)
	for _, field := range stmt.Schema.Fields {
		if field.DBName != "" {
			columnToField[strings.ToUpper(field.DBName)] = field
		}
	}

	// 处理单条记录或列表
	switch rv.Kind() {
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			processRecord(rv.Index(i), stmt.Schema, columnToField)
		}
	case reflect.Struct:
		processRecord(rv, stmt.Schema, columnToField)
	}
}

// processRecord 处理单条记录的字段值转换
func processRecord(rv reflect.Value, schemaInfo *schema.Schema, columnToField map[string]*schema.Field) {
	if !rv.IsValid() {
		return
	}

	// 确保是可寻址的值
	if rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	// 如果是指针，获取指向的元素
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			// 如果指针为nil，尝试创建一个实例
			if !rv.CanSet() {
				return
			}
			rv.Set(reflect.New(rv.Type().Elem()))
			rv = rv.Elem()
		} else {
			rv = rv.Elem()
		}
	}

	if rv.Kind() != reflect.Struct {
		return
	}

	// 预构建按 Go 字段名的映射
	nameToField := make(map[string]*schema.Field, len(schemaInfo.Fields))
	for _, f := range schemaInfo.Fields {
		nameToField[f.Name] = f
	}

	// 遍历结构体字段进行处理
	for i := 0; i < rv.NumField(); i++ {
		fieldStruct := rv.Type().Field(i)
		fieldValue := rv.Field(i)

		if !fieldValue.IsValid() || !fieldValue.CanSet() {
			continue
		}

		// 使用预构建的 map 查找
		schemaField := findSchemaFieldByStructFieldFast(schemaInfo, &fieldStruct, columnToField, nameToField)
		if schemaField == nil {
			continue
		}

		// 转换字段值
		convertedValue := convertFromOracleToField(fieldValue.Interface(), schemaField)
		if convertedValue != nil && !isZeroValue(convertedValue) {
			setFieldValue(fieldValue, convertedValue)
		}
	}
}

// findSchemaFieldByStructFieldFast 根据结构体字段快速查找 Schema 字段
func findSchemaFieldByStructFieldFast(schemaInfo *schema.Schema, structField *reflect.StructField, columnToField map[string]*schema.Field, nameToField map[string]*schema.Field) *schema.Field {
	// O(1) 查找 Go 字段名
	if field, ok := nameToField[structField.Name]; ok {
		return field
	}
	
	// 尝试通过数据库列名查找
	dbName := structField.Tag.Get("column")
	if dbName != "" {
		if schemaField, exists := columnToField[strings.ToUpper(dbName)]; exists {
			return schemaField
		}
	}
	
	// 尝试使用结构体字段名作为列名查找
	if schemaField, exists := columnToField[strings.ToUpper(structField.Name)]; exists {
		return schemaField
	}
	
	return nil
}

// setFieldValue 安全地设置字段值
func setFieldValue(fieldValue reflect.Value, value any) {
	if value == nil || !fieldValue.IsValid() || !fieldValue.CanSet() {
		return
	}

	// 获取值的反射值
	v := reflect.ValueOf(value)

	// 处理特殊情况：当目标字段是指针时
	if fieldValue.Kind() == reflect.Pointer {
		// 如果值是 nil，直接设置为 nil
		if value == nil {
			fieldValue.Set(reflect.Zero(fieldValue.Type()))
			return
		}

		// 如果目标是指针但值不是指针，需要创建一个指针
		if v.Kind() != reflect.Pointer {
			ptr := reflect.New(fieldValue.Type().Elem())
			ptr.Elem().Set(reflect.ValueOf(value))
			fieldValue.Set(ptr)
			return
		}

		// 如果都是指针，直接赋值
		if v.Type().AssignableTo(fieldValue.Type()) {
			fieldValue.Set(v)
			return
		}
	}

	// 处理目标字段不是指针的情况
	if fieldValue.Kind() == v.Kind() && v.Type().AssignableTo(fieldValue.Type()) {
		fieldValue.Set(v)
		return
	}

	// 如果类型不匹配，尝试进行类型转换
	if v.CanConvert(fieldValue.Type()) {
		fieldValue.Set(v.Convert(fieldValue.Type()))
		return
	}

	// 对于接口类型，可以直接赋值
	if fieldValue.Kind() == reflect.Interface {
		fieldValue.Set(v)
		return
	}
}

// isZeroValue 检查值是否为零值
func isZeroValue(value any) bool {
	if value == nil {
		return true
	}
	
	// 特殊处理 time.Time
	if t, ok := value.(time.Time); ok {
		return t.IsZero()
	}
	
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	case reflect.Struct:
		// 处理其他结构体类型的零值
		return v.IsZero()
	}

	return false
}
