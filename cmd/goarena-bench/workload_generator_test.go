package main

import (
	"bytes"
	"testing"
)

func TestAppendKey(t *testing.T) {
	seen := map[string]bool{}
	var k []byte
	for i := uint64(0); i < 10000; i++ {
		k = appendKey(k, i)
		if len(k) != keyLen || !bytes.HasPrefix(k, []byte("key:")) {
			t.Fatalf("bad key %q", k)
		}
		if seen[string(k)] {
			t.Fatalf("duplicate key for %d", i)
		}
		seen[string(k)] = true
	}
}

func TestKeyChooser(t *testing.T) {
	for _, dist := range []string{"uniform", "zipf"} {
		o := options{N: 1000, Dist: dist, ZipfS: 1.1, Seed: 3}
		kc := newKeyChooser(&o, 1)
		hits := map[uint64]int{}
		for i := 0; i < 100000; i++ {
			k := kc.next()
			if k >= 1000 {
				t.Fatalf("%s: key %d out of range", dist, k)
			}
			hits[k]++
		}
		if dist == "uniform" && len(hits) < 990 {
			t.Fatalf("uniform covers only %d keys", len(hits))
		}
		if dist == "zipf" {
			max := 0
			for _, c := range hits {
				if c > max {
					max = c
				}
			}
			if max < 5000 {
				t.Fatalf("zipf not skewed: max hits %d", max)
			}
		}
	}
}
