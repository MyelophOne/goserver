package goserver

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type GetValue struct{}
type GetValueOr struct{}

func (GetValue) String(m map[string]any, path string) (string, bool) {
	v, ok := GetDotPath(m, path)
	if !ok || v == nil {
		return "", false
	}

	switch val := v.(type) {
	case string:
		return val, true

	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), true

	case int:
		return strconv.Itoa(val), true

	case int64:
		return strconv.FormatInt(val, 10), true

	case bool:
		if val {
			return "true", true
		}
		return "false", true

	case json.Number:
		return val.String(), true
	}

	return "", false
}

func (GetValue) Int(m map[string]any, path string) (int, bool) {
	v, ok := GetDotPath(m, path)
	if !ok || v == nil {
		return 0, false
	}

	switch val := v.(type) {
	case int:
		return val, true

	case int64:
		return int(val), true

	case float64:
		return int(val), true

	case string:
		i, err := strconv.Atoi(val)
		return i, err == nil

	case json.Number:
		i, err := val.Int64()
		return int(i), err == nil
	}

	return 0, false
}

func (GetValue) Float(m map[string]any, path string) (float64, bool) {
	v, ok := GetDotPath(m, path)
	if !ok || v == nil {
		return 0, false
	}

	switch val := v.(type) {
	case float64:
		return val, true

	case int:
		return float64(val), true

	case int64:
		return float64(val), true

	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil

	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	}

	return 0, false
}

func (GetValue) Bool(m map[string]any, path string) (bool, bool) {
	v, ok := GetDotPath(m, path)
	if !ok || v == nil {
		return false, false
	}

	switch val := v.(type) {
	case bool:
		return val, true

	case string:
		b, err := strconv.ParseBool(val)
		return b, err == nil

	case int:
		return val != 0, true

	case int64:
		return val != 0, true

	case float64:
		return val != 0, true

	case json.Number:
		i, err := val.Int64()
		return i != 0, err == nil
	}

	return false, false
}

func (GetValueOr) String(m map[string]any, path string, def string) string {
	v, ok := GetValue{}.String(m, path)
	if !ok {
		return def
	}
	return v
}

func (GetValueOr) Int(m map[string]any, path string, def int) int {
	v, ok := GetValue{}.Int(m, path)
	if !ok {
		return def
	}
	return v
}

func (GetValueOr) Float(m map[string]any, path string, def float64) float64 {
	v, ok := GetValue{}.Float(m, path)
	if !ok {
		return def
	}
	return v
}

func (GetValueOr) Bool(m map[string]any, path string, def bool) bool {
	v, ok := GetValue{}.Bool(m, path)
	if !ok {
		return def
	}
	return v
}

func (GetValueOr) ToInt(v interface{}) (int, error) {
	switch val := v.(type) {

	case int:
		return val, nil

	case int8:
		return int(val), nil
	case int16:
		return int(val), nil
	case int32:
		return int(val), nil
	case int64:
		return int(val), nil

	case uint:
		return int(val), nil
	case uint8:
		return int(val), nil
	case uint16:
		return int(val), nil
	case uint32:
		return int(val), nil
	case uint64:
		return int(val), nil

	case float32:
		return int(val), nil
	case float64:
		return int(val), nil

	case string:
		n, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("cannot convert string to int: %w", err)
		}
		return n, nil

	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

func (GetValueOr) ToString(v interface{}) string {
	switch val := v.(type) {

	case string:
		return val

	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)

	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)

	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 64)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)

	case bool:
		return strconv.FormatBool(val)

	case fmt.Stringer:
		return val.String()

	default:
		return fmt.Sprintf("%v", v)
	}
}
