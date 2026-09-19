package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/qomos-w/spore/schema"
)

// Deserialize maps a parsed Config into a Go struct via reflection.
func Deserialize(cfg *Config, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("config: target must be a non-nil pointer to struct")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("config: target must be a pointer to struct, got %s", rv.Kind())
	}

	desc, err := schema.DescribeGoStruct(target)
	if err != nil {
		return fmt.Errorf("config: describe struct: %w", err)
	}

	cfgMap := make(map[string]KeyValue, len(cfg.Values))
	for _, kv := range cfg.Values {
		cfgMap[strings.ToLower(kv.Key)] = kv
	}

	for i, field := range desc.Fields {
		kv, ok := cfgMap[strings.ToLower(field.Name)]
		if !ok {
			continue
		}
		fv := rv.Field(i)
		if !fv.CanSet() {
			continue
		}
		if err := setValue(fv, kv.Value); err != nil {
			return fmt.Errorf("config: field %q: %w", field.Name, err)
		}
	}
	return nil
}

func setValue(fv reflect.Value, v Value) error {
	if fv.Kind() == reflect.Pointer {
		if v.Kind == ValueNull {
			fv.Set(reflect.Zero(fv.Type()))
			return nil
		}
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		return setValue(fv.Elem(), v)
	}

	switch v.Kind {
	case ValueInt:
		return setInt(fv, v.IntVal)
	case ValueFloat:
		return setFloat(fv, v.FloatVal)
	case ValueString:
		if fv.Kind() != reflect.String {
			return fmt.Errorf("cannot assign string to %s", fv.Type())
		}
		fv.SetString(v.StrVal)
		return nil
	case ValueBool:
		if fv.Kind() != reflect.Bool {
			return fmt.Errorf("cannot assign bool to %s", fv.Type())
		}
		fv.SetBool(v.BoolVal)
		return nil
	case ValueNull:
		fv.Set(reflect.Zero(fv.Type()))
		return nil
	case ValueArray:
		return setArray(fv, v)
	case ValueMap:
		return setMap(fv, v)
	case ValueStruct:
		return setStruct(fv, v)
	default:
		return fmt.Errorf("unsupported value kind %d", v.Kind)
	}
}

func setInt(fv reflect.Value, n int64) error {
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n < 0 {
			return fmt.Errorf("cannot assign negative value %d to %s", n, fv.Type())
		}
		fv.SetUint(uint64(n))
	default:
		return fmt.Errorf("cannot assign int to %s", fv.Type())
	}
	return nil
}

func setFloat(fv reflect.Value, f float64) error {
	switch fv.Kind() {
	case reflect.Float32, reflect.Float64:
		fv.SetFloat(f)
	default:
		return fmt.Errorf("cannot assign float to %s", fv.Type())
	}
	return nil
}

func setArray(fv reflect.Value, v Value) error {
	if fv.Kind() != reflect.Slice {
		return fmt.Errorf("cannot assign array to %s", fv.Type())
	}
	slice := reflect.MakeSlice(fv.Type(), len(v.Elements), len(v.Elements))
	for i, elem := range v.Elements {
		if err := setValue(slice.Index(i), elem); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
	}
	fv.Set(slice)
	return nil
}

func setMap(fv reflect.Value, v Value) error {
	if fv.Kind() == reflect.Struct {
		return setMapToStruct(fv, v)
	}
	if fv.Kind() != reflect.Map {
		return fmt.Errorf("cannot assign map to %s", fv.Type())
	}
	keyType := fv.Type().Key()
	valType := fv.Type().Elem()
	m := reflect.MakeMapWithSize(fv.Type(), len(v.Entries))
	for _, kv := range v.Entries {
		key := reflect.ValueOf(kv.Key)
		if key.Type() != keyType {
			ck, err := coerceStringTo(keyType, kv.Key)
			if err != nil {
				return fmt.Errorf("key %q: %w", kv.Key, err)
			}
			key = ck
		}
		val := reflect.New(valType).Elem()
		if err := setValue(val, kv.Value); err != nil {
			return fmt.Errorf("key %q: %w", kv.Key, err)
		}
		m.SetMapIndex(key, val)
	}
	fv.Set(m)
	return nil
}

func setMapToStruct(fv reflect.Value, v Value) error {
	fieldMap := make(map[string]int)
	ft := fv.Type()
	for i := 0; i < ft.NumField(); i++ {
		fieldMap[strings.ToLower(ft.Field(i).Name)] = i
	}
	for _, kv := range v.Entries {
		idx, ok := fieldMap[strings.ToLower(kv.Key)]
		if !ok {
			continue
		}
		field := fv.Field(idx)
		if !field.CanSet() {
			continue
		}
		if err := setValue(field, kv.Value); err != nil {
			return fmt.Errorf("field %q: %w", kv.Key, err)
		}
	}
	return nil
}

func setStruct(fv reflect.Value, v Value) error {
	if fv.Kind() != reflect.Struct {
		return fmt.Errorf("cannot assign struct %s to %s", v.TypeName, fv.Type())
	}
	return setMapToStruct(fv, Value{Kind: ValueMap, Entries: v.Fields})
}

func coerceStringTo(typ reflect.Type, s string) (reflect.Value, error) {
	if typ.Kind() == reflect.String {
		return reflect.ValueOf(s), nil
	}
	return reflect.Value{}, fmt.Errorf("cannot coerce string key to %s", typ)
}
