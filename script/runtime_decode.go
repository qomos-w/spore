package script

import (
	"fmt"
)

// This file holds boundary value conversion: decoding host-call results into Go
// destination pointers.

func decodeInto(dst any, value any) error {
	switch p := dst.(type) {
	case *any:
		*p = value
		return nil
	case *string:
		s, ok := value.(string)
		if !ok {
			return decodeTypeMismatch("string", value)
		}
		*p = s
		return nil
	case *bool:
		b, ok := value.(bool)
		if !ok {
			return decodeTypeMismatch("bool", value)
		}
		*p = b
		return nil
	case *int:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int(n)
		return nil
	case *int8:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int8(n)
		return nil
	case *int16:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int16(n)
		return nil
	case *int32:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = int32(n)
		return nil
	case *int64:
		n, err := decodeInt64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *uint:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint(n)
		return nil
	case *uint8:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint8(n)
		return nil
	case *uint16:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint16(n)
		return nil
	case *uint32:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = uint32(n)
		return nil
	case *uint64:
		n, err := decodeUint64(value)
		if err != nil {
			return err
		}
		*p = n
		return nil
	case *float32:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = float32(f)
		return nil
	case *float64:
		f, err := decodeFloat64(value)
		if err != nil {
			return err
		}
		*p = f
		return nil
	case *[]any:
		s, ok := value.([]any)
		if !ok {
			return decodeTypeMismatch("[]any", value)
		}
		*p = s
		return nil
	case *map[string]any:
		m, ok := value.(map[string]any)
		if !ok {
			return decodeTypeMismatch("map[string]any", value)
		}
		*p = m
		return nil
	}
	return fmt.Errorf("DecodeInto does not support target type %T", dst)
}

func decodeInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	}
	return 0, decodeTypeMismatch("integer", value)
}

func decodeUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case int:
		return uint64(v), nil
	case int8:
		return uint64(v), nil
	case int16:
		return uint64(v), nil
	case int32:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	}
	return 0, decodeTypeMismatch("unsigned integer", value)
}

func decodeFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	}
	return 0, decodeTypeMismatch("number", value)
}

func decodeTypeMismatch(target string, value any) error {
	if value == nil {
		return fmt.Errorf("DecodeInto: cannot decode nil into %s target", target)
	}
	return fmt.Errorf("DecodeInto: cannot decode %T into %s target", value, target)
}
