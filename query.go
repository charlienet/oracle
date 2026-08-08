package oracle

import (
	"reflect"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	gormSchema "gorm.io/gorm/schema"
)

// Query 是 Oracle 特定的查询回调函数
// 处理查询前后的数据转换和列名映射
func Query(db *gorm.DB) {
	stmt := db.Statement
	if stmt == nil {
		return
	}

	// 1. 查询前处理
	preprocessQuery(db)

	// 2. 执行查询（调用默认回调）
	callbacks.Query(db)

	// 3. 查询后处理
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

	// 在 Oracle 中，某些查询可能需要特定的 hint 或优化
	// 当前主要处理 LIMIT/OFFSET 重写（已在 ClauseBuilders 中处理）
	// 可以根据需要添加更多预处理逻辑
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
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	// 处理单条记录或列表
	switch rv.Kind() {
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			processRecord(rv.Index(i), stmt.Schema)
		}
	case reflect.Struct:
		processRecord(rv, stmt.Schema)
	}
}

// processRecord 处理单条记录的字段值转换
func processRecord(rv reflect.Value, schema *gormSchema.Schema) {
	if !rv.IsValid() {
		return
	}

	// 确保是可寻址的值
	if rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}

	// 如果是指针，获取指向的元素
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			// 如果指针为nil，尝试创建一个实例
			rv.Set(reflect.New(rv.Type().Elem()))
			rv = rv.Elem()
		} else {
			rv = rv.Elem()
		}
	}

	if rv.Kind() != reflect.Struct {
		return
	}

	// 创建列名到字段的映射，处理大小写问题
	columnToField := make(map[string]*gormSchema.Field)
	for _, field := range schema.Fields {
		if field.DBName != "" {
			// Oracle 默认返回大写列名，所以将字段的 DBName 转为大写作为键
			columnToField[strings.ToUpper(field.DBName)] = field
		}
	}

	// 遍历结构体字段进行处理
	for i := 0; i < rv.NumField(); i++ {
		fieldStruct := rv.Type().Field(i)
		fieldValue := rv.Field(i)

		if !fieldValue.IsValid() || !fieldValue.CanSet() {
			continue
		}

		// 查找对应的 Schema 字段
		schemaField := findSchemaFieldByStructField(schema, &fieldStruct, columnToField)
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

// findSchemaFieldByStructField 根据结构体字段查找 Schema 字段
func findSchemaFieldByStructField(schema *gormSchema.Schema, structField *reflect.StructField, columnToField map[string]*gormSchema.Field) *gormSchema.Field {
	// 首先尝试通过字段名查找
	for _, field := range schema.Fields {
		if field.Name == structField.Name {
			return field
		}
	}

	// 尝试通过数据库列名查找
	dbName := structField.Tag.Get("column")
	if dbName != "" {
		if schemaField, exists := columnToField[strings.ToUpper(dbName)]; exists {
			return schemaField
		}
	}

	// 如果上面都没找到，尝试使用结构体字段名作为列名查找
	if schemaField, exists := columnToField[strings.ToUpper(structField.Name)]; exists {
		return schemaField
	}

	return nil
}

// setFieldValue 安全地设置字段值
func setFieldValue(fieldValue reflect.Value, value interface{}) {
	if value == nil || !fieldValue.IsValid() || !fieldValue.CanSet() {
		return
	}

	// 获取值的反射值
	v := reflect.ValueOf(value)

	// 处理特殊情况：当目标字段是指针时
	if fieldValue.Kind() == reflect.Ptr {
		// 如果值是 nil，直接设置为 nil
		if value == nil {
			fieldValue.Set(reflect.Zero(fieldValue.Type()))
			return
		}

		// 如果目标是指针但值不是指针，需要创建一个指针
		if v.Kind() != reflect.Ptr {
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
func isZeroValue(value interface{}) bool {
	if value == nil {
		return true
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
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	}

	return false
}