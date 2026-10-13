package arena

import (
	"math/bits"
	"testing"
)

func TestSizeClassTable(t *testing.T) {
	cl := SizeClasses()
	if cl[0] != MinClassSize || cl[len(cl)-1] != MaxClassSize {
		t.Fatalf("class range %d..%d", cl[0], cl[len(cl)-1])
	}
	for i := 1; i < len(cl); i++ {
		if cl[i]%8 != 0 || cl[i] <= cl[i-1] {
			t.Fatalf("class %d = %d not increasing/aligned", i, cl[i])
		}
		if g := float64(cl[i]) / float64(cl[i-1]); g > 1.5 {
			t.Fatalf("growth %d->%d = %.2f too large", cl[i-1], cl[i], g)
		}
	}
	for n := 1; n <= MaxClassSize; n++ {
		c := ClassSize(n)
		if c < n {
			t.Fatalf("ClassSize(%d) = %d", n, c)
		}
		ci := classIndex(n)
		if ci > 0 && int(classSizes[ci-1]) >= n {
			t.Fatalf("n=%d not mapped to the smallest class", n)
		}
	}
	if ClassSize(0) != 0 || ClassSize(MaxClassSize+1) != MaxClassSize+8 {
		t.Fatal("ClassSize edge cases")
	}
	t.Logf("%d size classes: %v", len(cl), cl)
}

func TestHybridClassTable(t *testing.T) {
	cl := SizeClasses()
	if len(cl) != 201 {
		t.Fatalf("expected 201 classes, got %d", len(cl))
	}
	for i := 1; i < len(cl); i++ {
		prev, cur := cl[i-1], cl[i]
		switch {
		case cur <= LinearClassLimit:
			if cur-prev != 8 {
				t.Fatalf("linear part broken at %d: %d -> %d", i, prev, cur)
			}
		case prev < FineClassLimit:
			p := 1 << (bits.Len(uint(prev)) - 1)
			if cur-prev != p/FineSubclasses {
				t.Fatalf("log-linear part: %d -> %d, want step %d", prev, cur, p/FineSubclasses)
			}
		case cur < MaxClassSize:
			want := (int(float64(prev)*ClassGrowth) + 7) &^ 7
			if cur != want {
				t.Fatalf("geometric part: %d -> %d, want %d", prev, cur, want)
			}
		}
	}
	if ClassSize(156) != 160 || ClassSize(284) != 288 || ClassSize(1052) != 1056 || ClassSize(4124) != 4224 {
		t.Fatalf("ClassSize: %d %d %d %d", ClassSize(156), ClassSize(284), ClassSize(1052), ClassSize(4124))
	}
	// worst-case waste: < 8 B up to 256 B, < 1/32 up to 8 KiB, < ~20 % above
	for n := 17; n <= MaxClassSize; n++ {
		c := ClassSize(n)
		rel := float64(c-n) / float64(c)
		switch {
		case n <= LinearClassLimit && c-n >= 8:
			t.Fatalf("n=%d class %d: absolute waste %d", n, c, c-n)
		case n > LinearClassLimit && n <= FineClassLimit && rel >= 1.0/32:
			t.Fatalf("n=%d class %d: relative waste %.3f", n, c, rel)
		case n > FineClassLimit && rel >= 0.21:
			t.Fatalf("n=%d class %d: relative waste too high", n, c)
		}
	}
	if len(largeSizes) == 0 || largeSizes[len(largeSizes)-1] != MaxAlloc {
		t.Fatal("large classes must end at MaxAlloc")
	}
	for i, s := range largeSizes {
		if s%largeAlign != 0 && s != MaxAlloc {
			t.Fatalf("large class %d = %d not aligned", i, s)
		}
		if largeIndex(int(s)) != i || (i > 0 && largeIndex(int(largeSizes[i-1])+1) != i) {
			t.Fatalf("largeIndex inconsistent at %d", i)
		}
	}
}
