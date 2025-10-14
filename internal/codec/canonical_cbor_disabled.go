//go:build cbor_no_canonical

package codec

import (
	"errors"
	"reflect"

	"github.com/fxamacker/cbor/v2"
)

var ErrFloatDetected = errors.New("float values are not allowed in CBOR")

func hasFloat(rv reflect.Value) bool {
	if !rv.IsValid() {
		return false
	}
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return true
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return false
		}
		return hasFloat(rv.Elem())
	case reflect.Struct:
		for i := 0; i < rv.NumField(); i++ {
			if hasFloat(rv.Field(i)) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if hasFloat(rv.Index(i)) {
				return true
			}
		}
	case reflect.Map:
		for _, k := range rv.MapKeys() {
			if hasFloat(k) || hasFloat(rv.MapIndex(k)) {
				return true
			}
		}
	}
	return false
}

// 비캐노니컬 빌드에서도 시그니처는 동일하게 유지.
// (필요하면 -tags cbor_no_canonical 로 선택 사용)
func EncodeCBORCanonical(v any) ([]byte, error) {
	if hasFloat(reflect.ValueOf(v)) {
		return nil, ErrFloatDetected
	}
	return cbor.Marshal(v)
}
