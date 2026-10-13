package arena

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

// pointerFree caches the result of hasPointers per type.
var pointerFree sync.Map // reflect.Type -> bool

// hasPointers reports whether values of type t contain anything the Go
// garbage collector must trace: pointers, strings, slices, maps, channels,
// functions, interfaces or unsafe.Pointer, directly or in nested fields.
func hasPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return false
	case reflect.Array:
		return t.Len() > 0 && hasPointers(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if hasPointers(t.Field(i).Type) {
				return true
			}
		}
		return false
	default: // Pointer, UnsafePointer, String, Slice, Map, Chan, Func, Interface
		return true
	}
}

// checkType returns an error unless T may be stored in C memory.
func checkType[T any]() (reflect.Type, error) {
	t := reflect.TypeOf((*T)(nil)).Elem()
	v, ok := pointerFree.Load(t)
	if !ok {
		v = !hasPointers(t)
		pointerFree.Store(t, v)
	}
	if !v.(bool) {
		return t, fmt.Errorf("%w: %v", ErrPointerType, t)
	}
	if t.Align() > Alignment {
		return t, fmt.Errorf("arena: type %v needs alignment %d > %d", t, t.Align(), Alignment)
	}
	return t, nil
}

// IsPointerFree reports whether values of type T can be placed in arena
// memory, i.e. whether T contains no Go pointers.
func IsPointerFree[T any]() bool {
	_, err := checkType[T]()
	return err == nil
}

// NewValue allocates a zeroed T in arena memory and returns a pointer to it.
//
// T must be pointer-free (see [IsPointerFree]); otherwise NewValue returns
// ErrPointerType, because the garbage collector does not scan C memory and
// any Go pointer stored there could dangle. The returned pointer is valid
// until the arena is Reset or Released.
func NewValue[T any](a *Arena) (*T, error) {
	t, err := checkType[T]()
	if err != nil {
		return nil, err
	}
	size := int(t.Size())
	if size == 0 {
		return new(T), nil // zero-size values need no storage
	}
	ref, err := a.Alloc(size)
	if err != nil {
		return nil, err
	}
	b := a.Bytes(ref, size)
	clear(b)
	return (*T)(unsafe.Pointer(unsafe.SliceData(b))), nil
}

// NewSlice allocates a zeroed slice of n values of type T in arena memory.
// The same restrictions as for [NewValue] apply; n must be non-negative.
// The slice has length and capacity n; appending beyond n reallocates it in
// the Go heap, as for any other slice.
func NewSlice[T any](a *Arena, n int) ([]T, error) {
	t, err := checkType[T]()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, ErrInvalidSize
	}
	size := int(t.Size())
	if n == 0 || size == 0 {
		return make([]T, n), nil
	}
	if n > MaxAlloc/size {
		return nil, ErrInvalidSize
	}
	ref, err := a.Alloc(n * size)
	if err != nil {
		return nil, err
	}
	b := a.Bytes(ref, n*size)
	clear(b)
	return unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(b))), n), nil
}

// MustNewValue is like NewValue but panics on error.
func MustNewValue[T any](a *Arena) *T {
	v, err := NewValue[T](a)
	if err != nil {
		panic(err)
	}
	return v
}
