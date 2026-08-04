package output

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

var JSONMode bool

func JSON(v any) {
	data, _ := json.MarshalIndent(normalizeJSON(v), "", "  ")
	fmt.Println(string(data))
}

func normalizeJSON(v any) any {
	if v == nil {
		return v
	}
	return normalizeValue(reflect.ValueOf(v))
}

func normalizeValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return normalizeValue(v.Elem())
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return []any{}
		}
		items := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			items[i] = normalizeValue(v.Index(i))
		}
		return items
	case reflect.Map:
		if v.IsNil() {
			return map[string]any{}
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return v.Interface()
			}
			out[key.String()] = normalizeValue(iter.Value())
		}
		return out
	default:
		return v.Interface()
	}
}

func Error(msg string) {
	result := map[string]any{"success": false, "error": msg}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func Progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
