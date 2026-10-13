package arena

import (
	"errors"
	"testing"
	"unsafe"
)

type point struct {
	X, Y float64
	Tag  [4]uint16
}

type withString struct {
	ID   int
	Name string
}

type nested struct {
	P   point
	Arr [3]struct{ P *int }
}

func TestIsPointerFree(t *testing.T) {
	cases := []struct {
		name string
		got  bool
		want bool
	}{
		{"int", IsPointerFree[int](), true},
		{"point", IsPointerFree[point](), true},
		{"[8]byte", IsPointerFree[[8]byte](), true},
		{"[0]*int", IsPointerFree[[0]*int](), true},
		{"complex", IsPointerFree[complex128](), true},
		{"string", IsPointerFree[string](), false},
		{"[]byte", IsPointerFree[[]byte](), false},
		{"*int", IsPointerFree[*int](), false},
		{"map", IsPointerFree[map[int]int](), false},
		{"chan", IsPointerFree[chan int](), false},
		{"func", IsPointerFree[func()](), false},
		{"any", IsPointerFree[any](), false},
		{"unsafe.Pointer", IsPointerFree[unsafe.Pointer](), false},
		{"withString", IsPointerFree[withString](), false},
		{"nested", IsPointerFree[nested](), false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("IsPointerFree[%s] = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestNewValueAndSlice(t *testing.T) {
	a := MustNew(Config{ChunkSize: 1 << 16})
	defer a.Release()
	p, err := NewValue[point](a)
	if err != nil {
		t.Fatal(err)
	}
	if p.X != 0 || p.Tag[3] != 0 {
		t.Fatal("value not zeroed")
	}
	p.X, p.Tag[3] = 1.5, 9
	if uintptr(unsafe.Pointer(p))%8 != 0 {
		t.Fatal("misaligned value")
	}
	xs, err := NewSlice[uint64](a, 1000)
	if err != nil || len(xs) != 1000 || cap(xs) != 1000 {
		t.Fatalf("slice: %v len=%d", err, len(xs))
	}
	for i := range xs {
		if xs[i] != 0 {
			t.Fatal("slice not zeroed")
		}
		xs[i] = uint64(i)
	}
	if p.X != 1.5 || p.Tag[3] != 9 {
		t.Fatal("value overwritten by slice")
	}
	if _, err := NewValue[withString](a); !errors.Is(err, ErrPointerType) {
		t.Fatalf("expected ErrPointerType, got %v", err)
	}
	if _, err := NewSlice[*int](a, 4); !errors.Is(err, ErrPointerType) {
		t.Fatalf("expected ErrPointerType, got %v", err)
	}
	if _, err := NewSlice[int](a, -1); !errors.Is(err, ErrInvalidSize) {
		t.Fatal("negative length")
	}
	if _, err := NewSlice[int](a, MaxAlloc); !errors.Is(err, ErrInvalidSize) {
		t.Fatal("overflowing length")
	}
	if z, err := NewSlice[int](a, 0); err != nil || len(z) != 0 {
		t.Fatal("empty slice")
	}
	if e, err := NewValue[struct{}](a); err != nil || e == nil {
		t.Fatal("zero-size value")
	}
	if MustNewValue[int64](a) == nil {
		t.Fatal("MustNewValue")
	}
	small := MustNew(Config{ChunkSize: 4096, MaxBytes: 4096})
	defer small.Release()
	if _, err := NewSlice[byte](small, 4096); err != nil {
		t.Fatal(err)
	}
	if _, err := NewValue[point](small); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected OOM, got %v", err)
	}
	if _, err := NewSlice[point](small, 2); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected OOM, got %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustNewValue must panic for pointer types")
		}
	}()
	MustNewValue[string](a)
}
