package util

import (
	"fmt"
	"reflect"
)

// Simple serializer to get map as key.subkey=value with dot separation for the keys
func DotSerialize(prefix string, in interface{}) map[string]string {
	out := make(map[string]string)

	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Map {
		for _, k := range v.MapKeys() {
			prefix_key := fmt.Sprintf("%v", k.Interface())
			if len(prefix) > 0 {
				prefix_key = prefix + "." + prefix_key
			}
			int_out := DotSerialize(prefix_key, v.MapIndex(k).Interface())
			for key, val := range int_out {
				out[key] = val
			}
		}
	} else {
		out[prefix] = fmt.Sprintf("%v", in)
	}
	return out
}
