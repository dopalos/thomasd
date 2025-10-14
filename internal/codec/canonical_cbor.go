//go:build !cbor_no_canonical

package codec

import (
	"errors"
	"reflect"

	"github.com/fxamacker/cbor/v2"
)

// Canonical, deterministic CBOR encoder (float 금지)
var encMode cbor.EncMode

func init() {
	opts := cbor.CanonicalEncOptions() // canonical: key sort, definite lengths, etc.
	em, err := opts.EncMode()
	if err != nil {
		panic(err)
	}
	encMode = em
}

var ErrFloatDetected = errors.New("float values are not allowed in canonical CBOR")

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

func EncodeCBORCanonical(v any) ([]byte, error) {
	if hasFloat(reflect.ValueOf(v)) {
		return nil, ErrFloatDetected
	}
	return encMode.Marshal(v)
}
