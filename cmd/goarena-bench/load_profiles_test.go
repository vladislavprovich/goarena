package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildStages(t *testing.T) {
	names := func(o options) []int {
		st, err := buildStages(&o)
		if err != nil {
			t.Fatal(err)
		}
		var g []int
		for _, s := range st {
			g = append(g, s.Goroutines)
		}
		return g
	}
	eq := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	cases := []struct {
		profile string
		g       int
		want    []int
	}{
		{"steady", 16, []int{16}},
		{"", 4, []int{4}},
		{"ramp", 16, []int{1, 2, 4, 6, 8, 10, 12, 14, 16}},
		{"ramp", 4, []int{1, 2, 3, 4}},
		{"ramp", 1, []int{1}},
		{"sweep-goroutines", 16, []int{1, 2, 4, 8, 16}},
		{"sweep-goroutines", 12, []int{1, 2, 4, 8, 12}},
		{"spike", 8, []int{8, 8, 8, 8, 8, 8, 8, 8}},
	}
	for _, c := range cases {
		if got := names(options{Profile: c.profile, Goroutines: c.g, ReadRatio: 0.9}); !eq(got, c.want) {
			t.Errorf("%s G=%d: %v, want %v", c.profile, c.g, got, c.want)
		}
	}
	st, _ := buildStages(&options{Profile: "spike", Goroutines: 4, ReadRatio: 0.9, GarbageRate: 0})
	if st[1].Name != "write-spike" || st[1].ReadRatio > 0.51 || st[3].Name != "garbage-burst" || st[3].GarbageRate != 1 {
		t.Fatalf("spike stages %+v", st)
	}
	if _, err := buildStages(&options{Profile: "wave"}); err == nil {
		t.Fatal("unknown profile accepted")
	}
	o := options{Impl: "goarena", N: 10, ValueSize: 8, Goroutines: 1, ReadRatio: 0.5,
		Dist: "uniform", SampleEvery: 16, Shards: 4, ChunkMiB: 1, Profile: "wave"}
	if o.validate() == nil {
		t.Fatal("validate accepted an unknown profile")
	}
	o.Profile, o.Impl = "", "goarena-evict"
	if o.validate() == nil {
		t.Fatal("goarena-evict without -maxmem accepted")
	}
}

// TestRunWithProgressAndProfile runs a tiny benchmark with a load profile
// and progress output into a file and checks the progress lines, the
// per-stage statistics and the timeline of the result.
func TestRunWithProgressAndProfile(t *testing.T) {
	dir := t.TempDir()
	prog := filepath.Join(dir, "progress.jsonl")
	o := options{Impl: "goarena-evict", N: 20000, ValueSize: 64, Goroutines: 4, ReadRatio: 0.9,
		Dist: "zipf", ZipfS: 1.1, Duration: 2 * time.Second, Warmup: 300 * time.Millisecond,
		SampleEvery: 16, Shards: 16, ChunkMiB: 1, Seed: 1, Profile: "sweep-goroutines",
		MaxMemMiB: 2, FillOnMiss: true, ProgressOut: prog}
	res, err := run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stages) != 3 { // g = 1, 2, 4
		t.Fatalf("%d stages", len(res.Stages))
	}
	for i, s := range res.Stages {
		if s.Index != i || s.Ops == 0 || s.Latency.Count == 0 || s.Seconds <= 0 {
			t.Fatalf("stage %d: %+v", i, s)
		}
	}
	if res.Evictions == 0 || res.Store == nil || res.Store.ArenaReservedBytes > 2<<20 {
		t.Fatalf("eviction did not bound the store: evictions %d, store %+v", res.Evictions, res.Store)
	}
	f, err := os.Open(prog)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	phases := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var p progressPoint
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil {
			t.Fatalf("bad progress line %q: %v", sc.Text(), err)
		}
		if p.Type != "progress" || p.Impl != "goarena-evict" {
			t.Fatalf("bad point %+v", p)
		}
		phases[p.Phase]++
	}
	if phases["populate"] == 0 || phases["warmup"] == 0 || phases["measure"] < 3 {
		t.Fatalf("phases %v", phases)
	}
	if len(res.Timeline) != phases["populate"]+phases["warmup"]+phases["measure"] {
		t.Fatalf("timeline has %d points, file %v", len(res.Timeline), phases)
	}
}
