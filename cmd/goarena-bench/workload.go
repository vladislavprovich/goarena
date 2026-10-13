package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/savokh/goarena/gcpace"
	"github.com/savokh/goarena/gcstats"
	"github.com/savokh/goarena/internal/cmem"
	"github.com/savokh/goarena/internal/latency"
	"github.com/savokh/goarena/internal/tsc"
)

// options are the parameters of one benchmark run.
type options struct {
	Impl        string        `json:"impl"`
	N           int           `json:"n"`
	ValueSize   int           `json:"value_size"`
	Goroutines  int           `json:"goroutines"`
	ReadRatio   float64       `json:"read_ratio"`
	Dist        string        `json:"dist"`
	ZipfS       float64       `json:"zipf_s"`
	Duration    time.Duration `json:"duration_ns"`
	Warmup      time.Duration `json:"warmup_ns"`
	SampleEvery int           `json:"sample_every"`
	Shards      int           `json:"shards"`
	ChunkMiB    int           `json:"chunk_mib"`
	Seed        int64         `json:"seed"`
	Experiment  string        `json:"experiment"`
	Repeat      int           `json:"repeat"`
	MemLimitMiB int           `json:"memlimit_mib"` // debug.SetMemoryLimit; 0 = none
	GCTrace     bool          `json:"gctrace"`      // child runs with GODEBUG=gctrace=1

	// Request garbage (experiments E9, E10): with probability GarbageRate
	// an operation first allocates a short-lived "request" object (a
	// struct with pointers, a copy of the key and a GarbageBytes payload)
	// that stays reachable for the next 32 operations of the goroutine.
	// The same garbage is generated for every implementation; when it is
	// enabled the recorded latency covers allocation + cache operation.
	GarbageRate  float64 `json:"garbage_rate"`
	GarbageBytes int     `json:"garbage_bytes"`
}

// request simulates the per-request Go objects of a service.
type request struct {
	id    uint64
	key   []byte
	body  []byte
	owner *phaseResult
	tags  []string
}

// garbageRing is how many recent requests a goroutine keeps reachable.
const garbageRing = 32

var requestTags = []string{"GET", "/kv", "application/json"}

// phaseMarker writes a phase boundary to stderr when the process runs with
// GODEBUG=gctrace=1, so that the aggregator can attribute every gctrace
// line (which the runtime also writes to stderr) to a benchmark phase.
func phaseMarker(name string) {
	if strings.Contains(os.Getenv("GODEBUG"), "gctrace=1") {
		fmt.Fprintf(os.Stderr, "#phase %s\n", name)
	}
}

func (o *options) validate() error {
	if _, ok := impls[o.Impl]; !ok {
		return fmt.Errorf("unknown implementation %q (have %v)", o.Impl, implNames())
	}
	switch {
	case o.N <= 0:
		return fmt.Errorf("n must be positive")
	case o.ValueSize < 8:
		return fmt.Errorf("value size must be >= 8")
	case o.Goroutines <= 0:
		return fmt.Errorf("goroutines must be positive")
	case o.ReadRatio < 0 || o.ReadRatio > 1:
		return fmt.Errorf("read ratio must be in [0,1]")
	case o.Dist != "uniform" && o.Dist != "zipf":
		return fmt.Errorf("dist must be uniform or zipf")
	case o.SampleEvery <= 0 || o.SampleEvery&(o.SampleEvery-1) != 0:
		return fmt.Errorf("sample-every must be a power of two")
	case o.Shards <= 0 || o.Shards&(o.Shards-1) != 0:
		return fmt.Errorf("shards must be a power of two")
	case o.ChunkMiB <= 0:
		return fmt.Errorf("chunk size must be positive")
	case o.GarbageRate < 0 || o.GarbageRate > 1 || o.GarbageBytes < 0:
		return fmt.Errorf("garbage rate must be in [0,1] and garbage bytes >= 0")
	}
	return nil
}

// keyLen is the length of every benchmark key: "key:" + 16 hex digits.
const keyLen = 20

const hexDigits = "0123456789abcdef"

// appendKey writes the key of entry i into dst[:0]. Keys are a bijective
// scramble of i, so neighbouring indexes do not produce similar keys.
func appendKey(dst []byte, i uint64) []byte {
	x := i * 0x9E3779B97F4A7C15
	dst = append(dst[:0], "key:"...)
	for s := 60; s >= 0; s -= 4 {
		dst = append(dst, hexDigits[(x>>uint(s))&15])
	}
	return dst
}

// xorshift is a tiny per-goroutine PRNG (xorshift64*).
type xorshift uint64

func (x *xorshift) next() uint64 {
	v := uint64(*x)
	v ^= v >> 12
	v ^= v << 25
	v ^= v >> 27
	*x = xorshift(v)
	return v * 2685821657736338717
}

// keyChooser picks key indexes according to the configured distribution.
type keyChooser struct {
	n    uint64
	rng  xorshift
	zipf *rand.Zipf
}

func newKeyChooser(o *options, worker int) *keyChooser {
	seed := uint64(o.Seed)*1000003 + uint64(worker)*7919 + 1
	k := &keyChooser{n: uint64(o.N), rng: xorshift(seed | 1)}
	if o.Dist == "zipf" {
		r := rand.New(rand.NewSource(int64(seed)))
		k.zipf = rand.NewZipf(r, o.ZipfS, 1, uint64(o.N-1))
	}
	return k
}

func (k *keyChooser) next() uint64 {
	if k.zipf != nil {
		// scramble ranks so that hot keys are spread over all shards
		return (k.zipf.Uint64() * 0x9E3779B97F4A7C15) % k.n
	}
	return k.rng.next() % k.n
}

// fillValue writes deterministic, entry-specific content into v.
func fillValue(v []byte, i, version uint64) {
	x := xorshift(i*31 + version + 1)
	for j := 0; j+8 <= len(v); j += 8 {
		u := x.next()
		for b := 0; b < 8; b++ {
			v[j+b] = byte(u >> (8 * b))
		}
	}
}

// phaseResult is what the workers of one measured phase produce.
type phaseResult struct {
	Ops, Reads, Writes, Misses uint64
	All, ReadLat, WriteLat     *latency.Histogram
	Elapsed                    time.Duration
}

// populate inserts keys [0, N) using all goroutines.
func populate(c Cache, o *options) (time.Duration, error) {
	var wg sync.WaitGroup
	var firstErr atomic.Value
	per := (o.N + o.Goroutines - 1) / o.Goroutines
	start := time.Now()
	for w := 0; w < o.Goroutines; w++ {
		lo, hi := w*per, (w+1)*per
		if hi > o.N {
			hi = o.N
		}
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			key := make([]byte, 0, keyLen)
			val := make([]byte, o.ValueSize)
			for i := lo; i < hi; i++ {
				key = appendKey(key, uint64(i))
				fillValue(val, uint64(i), 0)
				if err := c.Set(key, val); err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
			}
		}(lo, hi)
	}
	wg.Wait()
	el := time.Since(start)
	if err, ok := firstErr.Load().(error); ok && err != nil {
		return el, err
	}
	return el, nil
}

// mixed runs the read/write mix for d with all goroutines. When measure is
// true, every SampleEvery-th operation is timed with the TSC clock.
func mixed(c Cache, o *options, d time.Duration, measure bool) phaseResult {
	var stop atomic.Bool
	var wg sync.WaitGroup
	results := make([]phaseResult, o.Goroutines)
	readThreshold := uint64(o.ReadRatio * float64(math.MaxUint32))
	nsPerTick := tsc.NsPerTick()
	sampleMask := uint64(o.SampleEvery - 1)
	garbageOn := o.GarbageRate > 0
	garbageThreshold := uint32(o.GarbageRate * float64(math.MaxUint32))
	start := time.Now()
	for w := 0; w < o.Goroutines; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := &results[w]
			r.All, r.ReadLat, r.WriteLat = latency.New(), latency.New(), latency.New()
			kc := newKeyChooser(o, w)
			opRng := xorshift(uint64(w)*0x51ED27 + uint64(o.Seed) + 99)
			key := make([]byte, 0, keyLen)
			val := make([]byte, o.ValueSize)
			buf := make([]byte, 0, o.ValueSize)
			var ops, reads, writes, misses uint64
			var ring [garbageRing]*request
			for !stop.Load() {
				// check the stop flag only every 64 operations
				for k := 0; k < 64; k++ {
					i := kc.next()
					key = appendKey(key, i)
					isRead := uint32(opRng.next()>>32) < uint32(readThreshold)
					timed := measure && ops&sampleMask == 0
					var t0, extra uint64
					if garbageOn && uint32(opRng.next()) < garbageThreshold {
						if timed {
							t0 = tsc.Ticks()
						}
						rq := &request{id: ops, key: append([]byte(nil), key...),
							body: make([]byte, o.GarbageBytes), owner: r, tags: requestTags}
						if len(rq.body) > 0 {
							rq.body[0] = byte(ops)
						}
						ring[ops%garbageRing] = rq
						if timed {
							extra = tsc.Ticks() - t0
						}
					}
					if isRead {
						if timed {
							t0 = tsc.Ticks()
						}
						var ok bool
						buf, ok = c.Get(buf, key)
						if timed {
							ns := uint64(float64(tsc.Ticks()-t0+extra) * nsPerTick)
							r.All.Record(ns)
							r.ReadLat.Record(ns)
						}
						if !ok {
							misses++
						}
						reads++
					} else {
						fillValue(val[:8], i, ops+1) // new version, same size
						if timed {
							t0 = tsc.Ticks()
						}
						_ = c.Set(key, val)
						if timed {
							ns := uint64(float64(tsc.Ticks()-t0+extra) * nsPerTick)
							r.All.Record(ns)
							r.WriteLat.Record(ns)
						}
						writes++
					}
					ops++
				}
			}
			r.Ops, r.Reads, r.Writes, r.Misses = ops, reads, writes, misses
			runtime.KeepAlive(ring)
		}(w)
	}
	time.Sleep(d)
	stop.Store(true)
	wg.Wait()
	total := phaseResult{All: latency.New(), ReadLat: latency.New(), WriteLat: latency.New(), Elapsed: time.Since(start)}
	for i := range results {
		r := &results[i]
		total.Ops += r.Ops
		total.Reads += r.Reads
		total.Writes += r.Writes
		total.Misses += r.Misses
		total.All.Merge(r.All)
		total.ReadLat.Merge(r.ReadLat)
		total.WriteLat.Merge(r.WriteLat)
	}
	return total
}

// startProfile starts a CPU profile if path is not empty and returns the
// function that stops it.
func startProfile(path string) (func(), error) {
	if path == "" {
		return func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { pprof.StopCPUProfile(); f.Close() }, nil
}

// run executes one complete benchmark: populate, warm up, measure.
func run(o options) (*Result, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	tsc.Calibrate()
	info := impls[o.Impl]
	res := newResult(o)

	// The memory limit is applied before the cache is created (v0.3.0;
	// earlier versions applied it right after New, which only allocates a
	// few empty shards) so that goarena-paced inherits it as its budget.
	if o.MemLimitMiB > 0 {
		debug.SetMemoryLimit(int64(o.MemLimitMiB) << 20)
	}
	c, err := info.New(o)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	res.GOGC = currentGOGC()

	cm0 := cmem.ReadStats()
	g0 := gcstats.Read()
	phaseMarker("populate-start")
	popTime, err := populate(c, &o)
	phaseMarker("populate-end")
	if err != nil {
		return nil, fmt.Errorf("populate: %w", err)
	}
	g1 := gcstats.Read()
	cm1 := cmem.ReadStats()
	res.PopulateSeconds = popTime.Seconds()
	res.PopulateOpsPerSec = float64(o.N) / popTime.Seconds()
	res.PopulateGC = gcstats.Between(g0, g1)
	res.PopulateCgoCalls = cm1.CgoCalls - cm0.CgoCalls
	res.AfterPopulate = g1.Gauges()
	if bc, ok := c.(interface{ afterPopulate() int64 }); ok {
		res.GoMemLimitMiB = bc.afterPopulate() >> 20
	}

	if o.Warmup > 0 {
		mixed(c, &o, o.Warmup, false)
	}

	g2 := gcstats.Read()
	cm2 := cmem.ReadStats()
	stopProfile, err := startProfile(cpuProfile)
	if err != nil {
		return nil, err
	}
	phaseMarker("measure-start")
	ph := mixed(c, &o, o.Duration, true)
	phaseMarker("measure-end")
	stopProfile()
	g3 := gcstats.Read()
	cm3 := cmem.ReadStats()

	res.Ops, res.Reads, res.Writes, res.Misses = ph.Ops, ph.Reads, ph.Writes, ph.Misses
	res.MeasuredSeconds = ph.Elapsed.Seconds()
	res.OpsPerSec = float64(ph.Ops) / ph.Elapsed.Seconds()
	res.Latency = ph.All.Summarize()
	res.ReadLatency = ph.ReadLat.Summarize()
	res.WriteLatency = ph.WriteLat.Summarize()
	res.LatencyBuckets = ph.All.Buckets()
	res.GC = gcstats.Between(g2, g3)
	res.GCCPUPercent = 100 * res.GC.GCCPUFraction
	res.After = g3.Gauges()

	// A forced, blocking full collection with the data set still live
	// measures the cost of one complete GC cycle (mark + sweep) directly.
	phaseMarker("forced-gc")
	t0 := tsc.Ticks()
	runtime.GC()
	res.ForcedGCms = float64(tsc.ToNanos(tsc.Ticks()-t0)) / 1e6
	phaseMarker("forced-gc-end")
	res.MeasuredCgoCalls = cm3.CgoCalls - cm2.CgoCalls
	res.OffHeapBytes = c.OffHeapBytes()
	res.CmemLiveBytes = cm3.LiveBytes
	if p := gcpace.Default(); p != nil {
		ps := p.Stats()
		res.Pacer = &ps
	}
	if ac, ok := c.(*arenaCache); ok {
		st := ac.storeStats()
		res.Store = &st
	}
	runtime.KeepAlive(c)
	return res, nil
}
