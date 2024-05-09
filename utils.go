package oracle

import (
	"database/sql"
	"reflect"
	"time"

	"gorm.io/gorm"
)

func convertCustomType(val interface{}) interface{} {
	rv := reflect.ValueOf(val)
	ri := rv.Interface()
	typeName := reflect.TypeOf(ri).Name()
	if reflect.TypeOf(val).Kind() == reflect.Ptr {
		if rv.IsNil() {
			typeName = rv.Type().Elem().Name()
		} else {
			for rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			ri = rv.Interface()
			typeName = reflect.TypeOf(ri).Name()
		}
	}
	if typeName == "DeletedAt" {
		// gorm.DeletedAt
		if rv.IsZero() {
			val = sql.NullTime{}
		} else {
			val = getTimeValue(ri.(gorm.DeletedAt).Time)
		}
	} else if m := rv.MethodByName("Time"); m.IsValid() && m.Type().NumIn() == 0 {
		// custom time type
		for _, result := range m.Call([]reflect.Value{}) {
			if reflect.TypeOf(result.Interface()).Name() == "Time" {
				val = getTimeValue(result.Interface().(time.Time))
			}
		}
	}
	return val
}

func ptrDereference(obj interface{}) (value interface{}) {
	if obj == nil {
		return obj
	}
	if t := reflect.TypeOf(obj); t.Kind() != reflect.Ptr {
		return obj
	}

	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() == reflect.Ptr && v.IsNil() {
		return obj
	}
	value = v.Interface()
	return
}

func getTimeValue(t time.Time) interface{} {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return t
}
