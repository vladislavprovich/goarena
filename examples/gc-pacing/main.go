// Command gc-pacing demonstrates store.Config.GCPacing on a service-style
// loop: every "request" allocates short-lived Go garbage and reads from a
// store that keeps tens of MiB off-heap.
//
// Without pacing the live Go heap is tiny (the data is off-heap), so the
// heap goal is tiny too and the garbage triggers a GC cycle every few MiB.
// With GCPacing the GC paces as if the off-heap bytes were part of the
// heap, and the same work runs with far fewer cycles. The example measures
// both variants with package gcstats.
//
//	go run ./examples/gc-pacing
package main

import (
	"fmt"
	"log"
	"runtime"

	"github.com/vladislavprovich/goarena/gcpace"
	"github.com/vladislavprovich/goarena/gcstats"
	"github.com/vladislavprovich/goarena/store"
)

const (
	entries   = 100_000 // ~ 50 MiB of values off-heap
	valueSize = 512
	requests  = 300_000
)

// request is the per-request Go garbage of a typical service: a struct
// with pointers, a copy of the key and a response buffer.
type request struct {
	key  []byte
	body []byte
	tags []string
}

// recent keeps the last few requests reachable, like in-flight requests.
var recent [64]*request

func key(i int) []byte { return fmt.Appendf(nil, "user:%08d", i) }

// result is what one variant measured.
type result struct {
	gc       gcstats.Delta // GC activity during the request loop
	heapGoal uint64        // heap size at which the next GC starts, at the end
	pacer    *gcpace.Stats // last pacer adjustment (GCPacing only)
}

func run(paced bool) result {
	s, err := store.New(store.Config{GCPacing: paced})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	val := make([]byte, valueSize)
	for i := 0; i < entries; i++ {
		if err := s.Set(key(i), val); err != nil {
			log.Fatal(err)
		}
	}
	if p := gcpace.Default(); p != nil {
		p.Update() // apply the populated size now instead of at the next tick
	}
	runtime.GC() // start both variants from the same state

	before := gcstats.Read()
	buf := make([]byte, 0, valueSize)
	for i := 0; i < requests; i++ {
		r := &request{key: key(i * 7919 % entries), body: make([]byte, 0, 2048), tags: []string{"GET", "/user"}}
		buf, _ = s.GetInto(buf, r.key)
		r.body = append(r.body, buf...)
		recent[i%len(recent)] = r
	}
	after := gcstats.Read()

	res := result{gc: gcstats.Between(before, after), heapGoal: after.HeapGoalBytes}
	if p := gcpace.Default(); p != nil {
		st := p.Stats()
		res.pacer = &st
	}
	return res
}

func main() {
	fmt.Printf("%d entries x %d B off-heap, %d requests allocating ~2 KiB of garbage each\n\n",
		entries, valueSize, requests)

	plain := run(false)
	paced := run(true)

	fmt.Printf("%-16s %10s %10s %15s\n", "variant", "GC cycles", "GC CPU %", "heap goal MiB")
	for _, r := range []struct {
		name string
		res  result
	}{{"GCPacing: false", plain}, {"GCPacing: true", paced}} {
		fmt.Printf("%-16s %10d %9.2f%% %15d\n",
			r.name, r.res.gc.GCCycles, 100*r.res.gc.GCCPUFraction, r.res.heapGoal>>20)
	}
	if ps := paced.pacer; ps != nil {
		fmt.Printf("\npacer: off-heap %d MiB, live heap %d MiB -> GC percent %d\n",
			ps.OffHeapBytes>>20, ps.LiveHeapBytes>>20, ps.GCPercent)
	}
	if n := paced.gc.GCCycles; n > 0 {
		fmt.Printf("GC cycles reduced %.0fx\n", float64(plain.gc.GCCycles)/float64(n))
	}
}
