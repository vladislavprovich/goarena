package store

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/vladislavprovich/goarena/internal/cmem"
)

// FuzzStore is a differential test: it interprets the fuzz input as a
// sequence of operations, applies them both to a Store and to a plain Go
// map (the model), and fails on the first observable difference. The store
// is configured with two shards and the minimum index size, so that short
// inputs already exercise collisions, tombstones, rehashing, in-place and
// relocating overwrites and the large-object path.
//
// Operation encoding (one byte opcode, then operands):
//
//	op%7 == 0,1  Set(key, value)    key: 1 byte selector, value: 1 byte length
//	op%7 == 2    Delete(key)
//	op%7 == 3    Get / GetInto / View(key)   (all three are compared)
//	op%7 == 4    SetWithTTL(key, value, ttl) and advance the fake clock
//	op%7 == 5    EvictExpired + Len + Range consistency check
//	op%7 == 6    Set(key, large value) (value above the largest size class)
func FuzzStore(f *testing.F) {
	f.Add([]byte{0, 1, 10, 3, 1, 2, 1, 3, 1})
	f.Add([]byte{0, 5, 200, 0, 5, 3, 0, 5, 250, 3, 5, 5})
	f.Add([]byte{6, 7, 0, 7, 3, 7, 2, 7, 3, 7})
	f.Add([]byte{4, 9, 30, 5, 3, 9, 5, 4, 9, 30, 0, 5, 5})
	f.Add(bytes.Repeat([]byte{0, 1, 2, 2, 1}, 40))

	f.Fuzz(func(t *testing.T, ops []byte) {
		live0 := cmem.ReadStats().LiveBlocks
		var clock int64 = 1 << 40
		old := nowNano
		nowNano = func() int64 { return clock }
		defer func() { nowNano = old }()

		s, err := New(Config{Shards: 2, ChunkSize: 64 << 10})
		if err != nil {
			t.Fatal(err)
		}
		type mval struct {
			v   []byte
			exp int64
		}
		model := map[string]mval{}
		alive := func(k string) (mval, bool) {
			m, ok := model[k]
			if ok && m.exp != 0 && clock >= m.exp {
				return m, false
			}
			return m, ok
		}
		keyOf := func(b byte) []byte {
			// 64 distinct keys of different lengths, incl. the empty key
			n := int(b%64) % 23
			k := make([]byte, n+3)
			k[0] = b % 64
			binary.LittleEndian.PutUint16(k[1:], uint16(b%64)*257)
			return k[:n]
		}
		valOf := func(seed byte, n int) []byte {
			v := make([]byte, n)
			for i := range v {
				v[i] = seed + byte(i)
			}
			return v
		}
		buf := make([]byte, 0, 16)
		next := func(i *int) byte {
			if *i >= len(ops) {
				return 0
			}
			c := ops[*i]
			*i++
			return c
		}
		for i := 0; i < len(ops); {
			op := next(&i) % 7
			kb := next(&i)
			key := keyOf(kb)
			switch op {
			case 0, 1:
				v := valOf(kb, int(next(&i))*int(op+1)*3)
				if err := s.Set(key, v); err != nil {
					t.Fatal(err)
				}
				model[string(key)] = mval{v: v}
			case 2:
				_, want := alive(string(key))
				if got := s.Delete(key); got != want {
					t.Fatalf("Delete(%x) = %v, model %v", key, got, want)
				}
				delete(model, string(key))
			case 3:
				m, want := alive(string(key))
				got, ok := s.Get(key)
				if ok != want || (ok && !bytes.Equal(got, m.v)) {
					t.Fatalf("Get(%x) = %v,%v; model %v,%v", key, len(got), ok, len(m.v), want)
				}
				buf, ok = s.GetInto(buf, key)
				if ok != want || (ok && !bytes.Equal(buf, m.v)) {
					t.Fatalf("GetInto(%x) mismatch", key)
				}
				viewed := s.View(key, func(v []byte) {
					if !bytes.Equal(v, m.v) {
						t.Fatalf("View(%x) mismatch", key)
					}
				})
				if viewed != want || s.Has(key) != want {
					t.Fatalf("View/Has(%x) = %v, model %v", key, viewed, want)
				}
			case 4:
				ttl := time.Duration(next(&i)%8+1) * time.Second
				v := valOf(kb, int(kb))
				if err := s.SetWithTTL(key, v, ttl); err != nil {
					t.Fatal(err)
				}
				model[string(key)] = mval{v: v, exp: clock + int64(ttl)}
				clock += int64(time.Second)
			case 5:
				s.EvictExpired()
				for k := range model {
					if _, ok := alive(k); !ok {
						delete(model, k)
					}
				}
				if s.Len() != len(model) {
					t.Fatalf("Len = %d, model %d", s.Len(), len(model))
				}
				n := 0
				s.Range(func(k, v []byte) bool {
					m, ok := model[string(k)]
					if !ok || !bytes.Equal(v, m.v) {
						t.Fatalf("Range: unexpected entry %x", k)
					}
					n++
					return true
				})
				if n != len(model) {
					t.Fatalf("Range visited %d, model %d", n, len(model))
				}
			case 6:
				v := valOf(kb, 70<<10+int(next(&i)))
				if err := s.Set(key, v); err != nil {
					t.Fatal(err)
				}
				model[string(key)] = mval{v: v}
			}
		}
		// final full comparison
		for k, m := range model {
			got, ok := s.Get([]byte(k))
			_, want := alive(k)
			if ok != want || (ok && !bytes.Equal(got, m.v)) {
				t.Fatalf("final Get(%x) mismatch", k)
			}
		}
		st := s.Stats()
		if st.LoadFactor > MaxLoadFactor {
			t.Fatalf("load factor %v", st.LoadFactor)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		// Other stores may be finalized concurrently, so only an increase
		// of live blocks is a leak of this iteration.
		if live := cmem.ReadStats().LiveBlocks; live > live0 {
			t.Fatalf("leaked %d C blocks", live-live0)
		}
	})
}
