# goarena design notes

This document explains every design decision of goarena, the exact memory
layouts (with byte offsets), the algorithms in prose, and the places where
the implementation deviates from the original specification. It is written
for readers who want to understand *why* the code looks the way it does.

Contents

1. Goals and the hybrid memory model
2. Package structure and dependency rules
3. The cgo bridge (`internal/cmem`)
4. The chunked arena (`arena.Arena`) and `Ref`
5. The size-class allocator (`arena.Slab`)
6. Typed helpers and the pointer check
7. The off-heap store (`store`)
8. Concurrency and memory-safety argument
9. Metrics (`gcstats`, `internal/latency`, `internal/tsc`)
10. Benchmark methodology (`cmd/goarena-bench`)
11. Example service and dashboard
12. Deviations from the specification
13. Toolchain notes (Windows, cgo, coverage, race detector, timers)

---

## 1. Goals and the hybrid memory model

The Go garbage collector is a concurrent, non-moving, non-generational
tri-color mark-and-sweep collector. The work of every cycle is proportional
to the amount of *scannable* live memory (objects that contain pointers),
and the frequency of cycles is governed by GOGC: a new cycle starts when the
heap reaches `live * (1 + GOGC/100)`. A cache that keeps millions of small
entries as Go objects (`map[string][]byte`) therefore pays twice: each cycle
must mark all entries (two pointers per entry) and the large live heap pushes
the process RSS to roughly twice the live size.

goarena applies a *hybrid* model: ordinary, short-lived Go data stays in the
Go heap, while large long-lived data sets are moved into memory obtained from
the C allocator through cgo. That memory is invisible to the GC: it is never
scanned, never counted towards the heap goal, and never freed implicitly.
The library's job is to make this safe and cheap:

* cheap - cgo transitions cost tens of nanoseconds, so C memory is requested
  in large chunks (64 MiB) and carved in Go code without crossing the
  boundary (amortisation);
* safe - C memory never contains Go pointers, references to it are plain
  integers (`Ref`), views (`[]byte`) never escape a lock scope inside the
  store, `Close` is idempotent and use-after-close is reported.

## 2. Package structure and dependency rules

```
github.com/savokh/goarena
├── internal/cmem      cgo bridge: malloc/calloc/free, counters (ONLY importer of "C")
├── internal/tsc       RDTSCP clock for latency sampling (Go assembly, amd64)
├── internal/latency   log-linear latency histogram
├── arena              Arena (chunks, bump pointer), Ref, Slab (size classes), NewValue/NewSlice
├── store              sharded off-heap KV store (index + entries in C memory)
├── gcstats            runtime/metrics + MemStats + OS process metrics
├── cmd/goarena-bench  benchmark harness, experiment driver, CSV aggregation
├── examples/httpcache HTTP service + live dashboard
├── scripts/           PowerShell + Python helpers
└── tools/term2png.py  renders captured console logs as PNG screenshots
```

Dependencies point downwards only: `store -> arena -> internal/cmem`;
`gcstats` depends only on the standard library; the harness and the example
depend on everything. Only the standard library is used (no third-party
modules); `go.mod` declares `go 1.22` because the example uses the
method-and-wildcard patterns of `net/http.ServeMux` introduced in Go 1.22.

## 3. The cgo bridge (`internal/cmem`)

### 3.1 API

`Malloc(n) unsafe.Pointer`, `Calloc(n)`, `Free(p)`, `SizeOf(p)`,
`Memclr(p, n)`, `Noop()` and `ReadStats() Stats`. Keeping all cgo calls in
one tiny package makes the cost of the boundary explicit (every call is
counted), keeps the rest of the code pure Go (faster builds, easier review),
and makes the "no Go pointers in C memory" rule auditable in one place.

### 3.2 Block header (memory layout)

Every block carries a hidden 16-byte header in front of the user pointer:

| offset | size | field | meaning |
|---|---|---|---|
| 0 | 8 | `size` | user size in bytes |
| 8 | 8 | `magic` | `0x676f6172656e6121` ("goarena!") while live, `0x6465657266726565` after `Free` |
| 16 | n | user area | returned pointer; 16-byte aligned (malloc alignment is 16 on amd64) |

Why: `Free(p)` needs no size argument (same contract as C `free`) while the
package still keeps exact live-byte accounting; and a magic check turns
`Free` of a foreign pointer or the most common double free into a Go panic
instead of silent heap corruption (best effort: after `free` the C allocator
may reuse the header). The cost is 16 bytes per block - negligible because
goarena allocates few, large blocks (chunks, index tables, large objects).

### 3.3 Counters

`CgoCalls`, `LiveBlocks`, `LiveBytes`, `PeakBytes`, `TotalAllocs`,
`TotalFrees` are `atomic.Int64` values, each padded to its own 64-byte cache
line to avoid false sharing between goroutines. They make three properties
testable: (1) no leaks - `LiveBlocks` returns to its previous value after
`Close`; (2) amortisation - the number of cgo calls per stored entry; (3)
off-heap footprint.

## 4. The chunked arena (`arena.Arena`) and `Ref`

### 4.1 `Ref` encoding

```
 63                              32 31                               0
+----------------------------------+----------------------------------+
|        chunk index + 1           |        byte offset in chunk      |
+----------------------------------+----------------------------------+
```

`Ref(0)` is nil. A Ref is a `uint64`, not a pointer, so it can be stored in C
memory or in pointer-free Go structures that the GC never scans. The offset
field limits a regular chunk to 4 GiB; the implementation caps chunks at
2 GiB (`MaxChunkSize`).

### 4.2 Chunk table

The arena keeps a table of chunks `{base unsafe.Pointer, size uintptr,
dedicated bool}`. The table is published through an `atomic.Pointer`: a
writer that needs a new slot copies the table into a bigger array (doubling)
and atomically stores the new header. Readers (`Bytes`, `Pointer`) do one
atomic load, one bounds check and one addition - no lock. The new slot is
written *beyond the published length* before publication, so a reader can
never observe a half-written entry. Freed dedicated slots are recycled
through a free-slot list.

### 4.3 Allocation algorithm (bump pointer)

`Alloc(n)`: round `n` up to 8 bytes; under the arena mutex, if the request
does not fit in the remainder of the current chunk, abandon the tail
(counted as `WastedBytes`) and switch to the next regular chunk - one kept by
a previous `Reset`, or a new one from `cmem.Malloc(ChunkSize)` (subject to
`MaxBytes`). Return `Ref(cur, off)` and advance `off`. Requests larger than
the chunk size receive a *dedicated chunk* of exactly their size. The
per-object cost is a mutex and a few arithmetic operations; the cgo cost is
paid once per chunk: with 64 MiB chunks and 192-byte blocks that is one cgo
call per ~350 000 entries.

Regular chunks are tracked in a separate list (`regs`) in allocation order.
An early version scanned the chunk table for "the next regular chunk",
which, after a dedicated chunk freed a slot that was then reused by a new
regular chunk, could hand out an already-used chunk a second time. The
randomized slab test caught the resulting overlap; the explicit list makes
the order unambiguous.

`Reset` frees dedicated chunks, keeps regular chunks and rewinds the bump
pointer (refs become invalid). `Release` frees everything and is idempotent;
afterwards all allocating methods return `ErrReleased`.

### 4.4 Why one arena per store (shared chunk pool)

A store has 256 shards. If every shard owned an arena with 64 MiB chunks, a
store would reserve at least 16 GiB of address space (and, on Windows, of
commit charge) before holding any meaningful data. Instead **one Arena per
store is shared by all shards** and acts as the common chunk pool; every
shard's Slab takes *spans* (pages) of 64 KiB from it (section 5.3). Reserved
memory is therefore `ceil(used / 64 MiB) * 64 MiB` plus at most one partly
used span per shard (256 x 64 KiB = 16 MiB worst case). This is the
"shared chunk source" variant of formula (2.17) in chapter 2. The benchmark
JSON reports both reserved (`store.ArenaReservedBytes`) and used
(`store.SlabInUseBytes`, `store.EntryBytes`) memory.

## 5. The size-class allocator (`arena.Slab`)

### 5.1 Size classes (log-linear table, v0.3.0)

History: v0.1.0 used a purely geometric table `c(0) = 16`,
`c(i+1) = 8*ceil(1.25*c(i)/8)` (36 classes); the 164-byte entries of the main
experiment wasted 14.6 % (class 192). v0.2.0 added an 8-byte linear part up
to 256 B (56 classes; 2.5 % for the 156-byte entries) but kept the x1.25
steps above 256 B, which cost 11-15 % for 256 B - 4 KiB values and made
goarena use 3-17 % more memory than the pointer-free Go-heap baseline in E4.

v0.3.0 uses a **three-part, log-linear** table:

| range | step | classes | worst-case waste |
|---|---|---|---|
| 16 B - 256 B | 8 B | 31 | < 8 B (absolute) |
| 256 B - 8 KiB | 2^k / 32 within [2^k, 2^(k+1)): 8, 16, 32, 64, 128 B | 160 | < 1/32 = 3.1 % |
| 8 KiB - 64 KiB | x1.25, rounded up to 8 B | 10 | ~20 % |

Altogether **201 classes** (still fits the one-byte `classOf8` lookup table,
so `classIndex` stays one load). The upper part ends with
`... 7808 7936 8064 8192 10240 12800 16000 20000 25000 31256 39072 48840 61056 65536`.

Candidates evaluated before the choice (mean internal waste for uniformly
distributed request sizes; waste of the E4 entries with 64/256/1024/4096-byte
values = 92/284/1052/4124-byte entries):

| table | classes | mean 17 B-64 KiB | mean 257 B-4 KiB | max 257 B-8 KiB | E4 entries |
|---|---|---|---|---|---|
| v0.2.0 hybrid | 56 | 9.62 % | 10.54 % | 20.4 % | 4.2 / 11.2 / 15.2 / 13.2 % |
| 32 B steps to 1 KiB, 128 B to 4 KiB | 92 | 9.06 % | 2.76 % | 20.0 % | 4.2 / 1.4 / 8.7 / 19.5 % |
| 32/64/128/256 B steps to 8 KiB | 113 | 8.58 % | 2.22 % | 10.8 % | 4.2 / 1.4 / 3.3 / 5.2 % |
| 16 classes per power of two to 8 KiB | 121 | 8.57 % | 2.08 % | 5.9 % | 4.2 / 1.4 / 3.3 / 5.2 % |
| **32 classes per power of two to 8 KiB (chosen)** | **201** | **8.44 %** | **1.04 %** | **3.0 %** | **4.2 / 1.4 / 0.4 / 2.4 %** |

The fixed-step proposal (32 B to 1 KiB, 128 B to 4 KiB) was rejected
because a 4 KiB value plus header (4124 B) falls just above the last fine
class into the geometric part (19.5 % waste). The cost of 201 classes is
per-slab state: 201 x 40 B = 8 KiB per shard, about 2 MiB of (pointer-free,
never scanned) Go heap per store with 256 shards - a constant that does not
grow with the number of entries (`TestOffHeapIsInvisibleToGC` now measures
the growth relative to an empty store). More classes also mean more
partially used free lists after deletions of mixed sizes; for the
same-size overwrites of the benchmarks this has no effect.

### 5.2 Free lists (intrusive)

Every class has a LIFO free list whose links are stored *inside* the freed
blocks: the first 8 bytes of a free block hold the `Ref` of the next free
block. The minimum class (16 B) guarantees room for the link. No side
structure, no Go pointers, O(1) push/pop. LIFO order reuses the most
recently freed (cache-warm) block first.

### 5.3 Spans, donations and the three allocation paths

1. *Small classes* (size <= span/4 = 16 KiB): blocks are carved from the
   current span. A new span is taken, in this order, from the regions
   donated to the slab (`Slab.Donate`, see 7.3: outgrown index tables) or as
   a fresh 64 KiB region from the shared arena by one `Arena.Alloc`. Spans
   batch the arena mutex: one lock per ~400 blocks of 160 B. A span tail too
   small for the next block, and a donated region too small for the class
   being refilled, are abandoned (`SpanWaste`).
2. *Mid classes* (16 KiB < size <= 64 KiB): allocated directly from the
   arena, so a span is never wasted on a few large blocks.
3. *Large objects* (> 64 KiB): one dedicated arena chunk each
   (`AllocDedicated`). With the default configuration they are returned to
   C by `Free`. With `SlabConfig.RetainLarge` (used by the store) they are
   rounded up to **large classes** (geometric x1.25 from 64 KiB, rounded to
   4 KiB, 47 classes up to 2 GiB) and freed blocks go to per-class free
   lists instead of `free()`. Memory therefore never leaves the arena before
   `Release`, which is a precondition of the lock-free read path (8.3). The
   cost is up to 25 % (+4 KiB) rounding on large values and the retention of
   large blocks until reused by the same class.

### 5.4 Statistics

Per class: block size, blocks in use, blocks on the free list, blocks ever
carved, requested bytes of in-use blocks. Aggregates: in-use bytes
(class-rounded), requested bytes, free-list bytes (incl. retained large
blocks), span bytes, donated bytes, span waste, large objects; internal
fragmentation `1 - requested / in-use`.

## 6. Typed helpers and the pointer check

`NewValue[T]` and `NewSlice[T]` place values of type `T` in arena memory.
Because the GC never scans C memory, a Go pointer stored there would not keep
its target alive and could dangle. The helpers therefore walk `T` with
`reflect` and reject it (`ErrPointerType`) if it contains pointers, strings,
slices, maps, channels, functions, interfaces or `unsafe.Pointer` at any
depth (zero-length arrays are allowed). The verdict is cached per type in a
`sync.Map`, so the check costs one map lookup after the first call. Types
that need an alignment above 8 bytes are rejected as well. Memory is zeroed
(`clear`) to honour Go's zero-value semantics.

## 7. The off-heap store (`store`)

### 7.1 Structure (v0.2.0)

```
Store
 ├── seed (maphash.Seed), shardShift, defaultTTL, closed (atomic.Bool)
 ├── readers [64]readerStripe        ← in-flight optimistic readers (8.3)
 ├── arena *arena.Arena              ← shared chunk pool (C memory): entries AND index tables
 └── shards [256]shard               ← 192 bytes = 3 cache lines each
```

`shard` memory layout (offsets in bytes; the size is a multiple of 64, the
shard array is page-aligned, so every shard starts on a cache line):

| offset | size | field | used by |
|---|---|---|---|
| 0 | 8 | `seq atomic.Uint64` - seqlock sequence (odd while a writer mutates) | readers (load), writers |
| 8 | 8 | `tab atomic.Pointer[indexTable]` - current index table descriptor | readers, writers |
| 16 | 8 | `a *arena.Arena` - for resolving refs | readers, writers |
| 24 | 40 | padding (line 0 holds only read-mostly fields) | |
| 64 | 8 | `mu shardLock` (a `sync.Mutex`) - writer lock | writers, slow-path readers |
| 72 | 8 | `slab *arena.Slab` | writers |
| 80 | 16 | `live`, `tombs` | writers |
| 96 | 16 | `kvBytes`, `entryBytes` | writers |
| 112 | 24 | `sets`, `deletes`, `expired` | writers |
| 136 | 56 | padding to 192 | |

The Go heap holds the `Store`, 256 shards (48 KiB), 256 `Slab` objects, 256
tiny immutable `indexTable` descriptors and the 4 KiB reader stripes - a
constant, independent of the number of entries (measured Go heap: 2-3 MiB).

### 7.2 Hashing and shard selection

The key is hashed once with `hash/maphash` (`maphash.Bytes`, AES-based on
amd64) using a per-store random seed created in `New` (no re-seeding per
call; the seed also defeats hash-flooding). The top `log2(shards)` bits
select the shard, the low bits select the home slot of the shard's index, so
the two choices are statistically independent. The values 0 and 1 are
reserved (empty, tombstone): a real hash below 2 is remapped by adding 2.
Profiling of v0.2.0 shows hashing at ~2 % of CPU; the 64-bit hash stored in
every slot already acts as a fingerprint (a key comparison happens only when
the full hash matches, i.e. practically only on hits), so no separate tag
byte is needed.

### 7.3 Index: open addressing with linear probing

Slot layout (16 bytes, arena memory, zeroed when the table is created):

| offset | size | field | meaning |
|---|---|---|---|
| 0 | 8 | `hash` | full 64-bit hash; 0 = empty, 1 = tombstone |
| 8 | 8 | `ref` | `arena.Ref` of the entry (0 for empty/tombstone) |

`indexTable{slots unsafe.Pointer, ref arena.Ref, mask uint64}` is an
immutable Go descriptor published through `shard.tab` (atomic pointer), so a
lock-free reader always sees a matching (base, mask) pair.

* Lookup: start at `hash & mask`, step by +1 (wrapping) until an empty slot
  (miss) or a slot with the same full hash whose entry key equals the
  requested key (hit). A lookup touches the slot array and, on a hit,
  exactly one entry (two cache misses for a random key).
* Insert: probe for the first empty or tombstone slot.
* Growth: before inserting, if `(live + tombstones + 1) / capacity > 0.75`
  the table is rebuilt; it doubles when live entries alone exceed 3/8 of the
  capacity, otherwise it is rebuilt at the same size, which only purges
  tombstones (deviation 12.3). Rebuilding needs no access to entries because
  slots carry full hashes.
* **v0.2.0: tables live in the arena and are recycled.** In v0.1.0 tables
  were separate `calloc` blocks freed on growth. Now a table is an arena
  allocation (`newTable`), and the outgrown table is **donated** to the
  shard's slab (`Slab.Donate`), which carves future entries from it. This
  (a) never unmaps memory that a concurrent optimistic reader may still be
  reading, (b) wastes nothing (the doubling series of old tables becomes
  entry memory), and (c) puts the index under `Config.MaxMemory`.
* Delete: the slot becomes a tombstone - unless the next slot is empty, in
  which case it (and the run of tombstones directly before it) can be turned
  back into empty slots, since every probe sequence through them would stop
  at the following empty slot anyway.
* Initial capacity: 64 slots per shard (1 KiB), or enough for
  `Config.InitialCapacity / shards` entries at load factor 0.75.

Linear probing was chosen over chaining (no per-entry node allocation, one
contiguous array, cache-friendly scans) and over Robin Hood/cuckoo hashing
(simpler deletes, good enough probe lengths at 0.75). Measured: average
displacement 0.8 slots at load factor ~0.6 (`Stats.AvgProbeDistance`).

Index memory per entry: 16 B / load factor; with doubling the load factor
lies in (0.375, 0.75], i.e. 21-43 B per entry (26.8 B at N = 10M, load 0.6).
A smaller slot (e.g. 8 bytes: 32-bit tag + 32-bit compressed ref) was
considered and rejected for this version: it would limit the arena to 2^32
x 8 B and require a second access to verify the tag, and the measured RSS
already falls below the pointer-free Go-heap baseline.

### 7.4 Entry layout (slab memory, v0.2.0)

| offset | size | field |
|---|---|---|
| 0 | 2 | `keyLen` uint16 (keys up to 65 535 B) |
| 2 | 2 | `flags` uint16 - bit 0: entry has a TTL |
| 4 | 4 | `valLen` uint32 |
| 8 | 8 | `expireAt` int64, Unix ns - **present only if flags bit 0 is set** |
| 8 or 16 | keyLen | key bytes |
| ... | valLen | value bytes |

Entries without a TTL (the common case) carry an **8-byte** header (v0.1.0:
16 bytes always). Entries are 8-byte aligned (slab blocks are), so the header
fields are naturally aligned. Total size `8 [+8] + keyLen + valLen`, rounded
up to a size class. Limits: key <= 65 535 B (v0.1.0: 65 536), entry <= 1 GiB
(`ErrTooLarge`). Combined with the hybrid classes, the 20 B + 128 B entry of
the benchmarks needs 160 B instead of 192 B (-16.7 %).

### 7.5 Operations

* `Set`: hash, take the shard's writer lock, check `closed`, look the key
  up. Allocation of a new block and writing the new entry happen *before*
  the sequence number is made odd (the block is not yet reachable). Then
  `seq++` (odd), the slot is updated / the entry overwritten in place / the
  old block freed / the table rehashed, `seq++` (even), unlock. Same block
  size -> overwrite in place, no allocation. Allocation failure maps to
  `ErrNoMemory` (the old value stays intact).
* `Get`, `GetInto`, `GetString`, `Has`: lock-free seqlock read (8.3), with
  fallback to the shard lock after 4 failed attempts. `GetInto` appends into
  a caller buffer (zero allocations; used by the benchmark).
* `View` and `Range` hold the shard lock while the callback runs (zero
  copy; `defer` unlock in case the callback panics). `Len` and `Stats` lock
  shards one at a time.
* TTL: `expireAt` is checked lazily on reads (`time.Now` is called only if
  the entry actually has a TTL); `EvictExpired` sweeps all shards and frees
  expired entries. There is no background goroutine, so a dropped store is
  collectable and its finalizer can run.
* `Close`: takes `closeMu`, then **all** shard locks (waiting for writers and
  slow-path readers), sets `closed`, then **waits until every reader stripe
  is zero** (optimistic readers in flight), then releases the arena (entries
  and index tables), then unlocks. Idempotent. After `Close`, writes return
  `ErrClosed` and reads report "not found".
* Finalizer: `runtime.SetFinalizer(store, ...)` closes a store that became
  unreachable without `Close` and increments `FinalizedStores()`. `Close`
  clears the finalizer.

### 7.6 Writer lock (v0.2.0)

v0.1.0 protected each shard with a `sync.RWMutex`: every reader incremented
the shard's `readerCount` (an atomic read-modify-write on one cache line),
so the readers of a hot key serialised on that line, and writers of hot
shards were parked on a semaphore (in a 5M-entry Zipf run the write p99.9
was 223 us). In v0.2.0 readers normally take no lock (8.3), so the shard
lock serves writers and the rare slow-path reader only, and a plain
`sync.Mutex` (wrapped as `shardLock`) replaces the RWMutex. `sync.Mutex`
spins briefly before it parks, which suits critical sections of ~100-300 ns.

Two alternatives were measured during the optimisation round (8-10 s runs,
same machine, not part of the reported matrix):

| writer lock | Zipf 5M: Mops/s | Zipf read / write p99.9 | uniform 1M: ops > 1 ms (of ~38M samples) | max |
|---|---|---|---|---|
| `sync.RWMutex` + readers on the lock-free path | 40.1 | 1.42 / 223 us | - | 3.1 ms |
| spinning TTAS lock with `Gosched` | 50.7 | 0.87 / 1.14 us | 113 | 20.6 ms |
| `sync.Mutex` (adopted) + reader wait-for-even-seq | 51.6 | 0.84 / 1.55 us | 57 | 6.7 ms |

The spinning lock never releases the CPU; with 16 busy goroutines on 16
logical CPUs the operating system then has to preempt benchmark threads to
run anything else, and a preempted lock holder (or reader) stalls for a
scheduler quantum, which produced multi-millisecond outliers. The same
effect explains the remaining outliers of the adopted variant: goarena v0.2.0
never blocks on the read path, so the process uses ~15 of 16 CPUs (149.6 CPU
seconds in a 10 s run vs 138.6 for gobytes); with 14 goroutines the number of
operations slower than 1 ms drops from 57 to 2 (max 1.24 ms).

## 8. Concurrency and memory-safety argument

1. *No Go pointers in C memory.* C memory contains only: chunk payloads
   (entries = integers + copied bytes), index slots (two `uint64`), free-list
   links (`Ref`), cmem headers (two `uint64`). Go structures may hold
   pointers *to* C memory (`unsafe.Pointer` chunk bases), which is allowed:
   the GC ignores pointers outside its heap.
2. *Views never outlive locks or validation.* Locked paths create and
   consume entry views under the shard lock; `View`/`Range` document that
   the callback must not retain the slice. The optimistic path copies the
   value and validates the copy before returning it.
3. **Seqlock read path (v0.2.0).** A reader (`readOptimistic`):
   1. loads `seq` (atomic); while it is odd (a writer is inside its short
      critical section) it re-reads it up to 256 times before counting the
      attempt as failed;
   2. loads the table descriptor `tab` (atomic) and probes slots with
      `atomic.LoadUint64` (plain `MOV` on amd64);
   3. on a matching hash loads `ref`, and resolves it with
      `Arena.TryPointer(ref, 8)` - a non-panicking, bounds-checked lookup
      in the immutable chunk table; a garbage ref is rejected (retry);
   4. reads `keyLen`, `flags`, `valLen` **once** into locals and checks
      `TryPointer(ref, header+keyLen+valLen)`; every later access (key
      compare, `expireAt`, value copy) is derived from these locals and
      therefore stays inside a live chunk even if the memory changes
      concurrently. (The first implementation re-read the header for the
      copy; the new torn-read test crashed on a garbage length within
      seconds - fixed before any measurement.);
   5. compares the key, re-checks `seq` (header and key were consistent),
      evaluates the TTL, copies the value into the caller's buffer (the
      length is the validated one), and checks `seq` a final time. Any
      change means a writer intervened: the result is discarded and the
      read retried (up to 4 times, then the locked slow path).
   Writers bracket every mutation of slots or entry memory with `seq++`
   (odd) ... `seq++` (even) under the writer lock. On amd64 (TSO) stores are
   not reordered with other stores and loads not with other loads, and Go's
   atomic operations are sequentially consistent, so a reader that observes
   the same even `seq` before and after its reads observed no partial
   mutation (standard seqlock argument).
4. **Why speculative reads cannot fault.** A torn read may follow a stale
   slot or a garbage ref, but only into memory that is still mapped:
   (a) arena chunks are released only by `Close`; (b) outgrown index tables
   are donated to the slab instead of freed (7.3); (c) large objects are
   retained on large-class free lists (`RetainLarge`, 5.3); (d) freed small
   blocks stay in their chunk; (e) the chunk table entries of a live arena
   are never modified once published (only `Reset`/`Release`/
   `FreeDedicated` modify them, and the store uses none of them while open),
   so `TryPointer` itself is race-free; (f) the table descriptor is an
   immutable Go object kept alive by the GC as long as a reader references
   it. Probe loops are bounded by the table capacity, so garbage slots
   cannot make a reader loop forever.
5. **Close vs in-flight readers.** An optimistic reader increments one of
   64 padded *reader stripes* (chosen by hashing the address of a stack
   variable, which differs between goroutines, so readers of the same hot
   key do not share a counter) and then checks `closed`; `Close` sets
   `closed` and then waits until all stripes are zero. With sequentially
   consistent atomics either the reader sees `closed` (and backs out) or
   `Close` sees the reader's increment (and waits) - a Dekker-style
   handshake. Readers decrement the stripe *before* falling back to the
   lock, so `Close` (which holds all locks) cannot deadlock with them.
6. *Data races in the Go-memory-model sense.* The speculative reads of C
   memory race with writers by design (as in every seqlock); the race
   detector does not instrument non-Go memory, and every Go-memory
   variable shared with readers (`seq`, `tab`, stripes, `closed`) is
   accessed atomically. `go test -race` passes, including the new tests.
7. *Bounds checks.* `Arena.Bytes` validates the chunk index, the offset and
   the length against the chunk size and panics like an out-of-range slice
   index; `TryPointer` performs the same checks without panicking.
8. *Alignment.* Arena allocations are 8-byte aligned; cmem blocks 16-byte.
   Go's `checkptr` instrumentation (enabled by `-race`) found no violation.
9. *Verification.* Unit tests, a differential fuzz test against a Go map
   model (TTL, overwrite across classes, large objects, Range, Len), a
   concurrency stress test with self-validating values, the v0.2.0 tests
   `TestOptimisticReadsNeverTorn` (writers relocate values across classes
   and into the large path, insert/delete churn forces rehashes and table
   donation; readers verify a CRC32 in every value and must always find the
   never-deleted keys), `TestHotKeyReaders`, `TestReadOptimisticRejectsGarbage`
   (bogus refs, odd sequence, exhausted probe), `TestShardLayout`, a
   concurrent-Close test (`Get` is now optimistic, so it exercises the
   stripe handshake), leak tests based on cmem counters, and the race
   detector (section 13.3).

## 9. Metrics

### 9.1 `gcstats`

`Read()` samples runtime/metrics (`/cpu/classes/gc/total`, `/cpu/classes/
total`, `/cpu/classes/idle`, `/gc/cycles/total`, `/gc/heap/live`, `/gc/heap/
goal`, `/gc/scan/heap`, `/memory/classes/total`, the STW histogram
`/sched/pauses/total/gc:seconds` with a fallback to `/gc/pauses:seconds`)
plus `runtime.ReadMemStats` (exact `PauseTotalNs` and the per-cycle
`PauseNs` ring buffer) and the OS process view: on Windows
`K32GetProcessMemoryInfo` (working set = RSS, peak working set, private
bytes) and `GetProcessTimes` called through the standard `syscall` package
(no `golang.org/x/sys`), on Linux `/proc/self/status` and `getrusage`.
`Between(a, b)` returns cycles, GC CPU seconds, **GC CPU fraction = GC CPU
time / (GOMAXPROCS x wall time)** - the quantity the GC pacer controls -
GC share of non-idle CPU, assist CPU, STW events, histogram-based maximum
STW event, exact total STW time and exact maximum per-cycle STW time.

### 9.2 `internal/latency`

Log-linear histogram: values below 32 ns are exact; above, each power of two
is split into 32 linear sub-buckets, bounding the relative quantile error by
3.1 %. 1 920 `uint64` counters (15 KiB), no allocation when recording,
mergeable, exportable as buckets (used for CDF plots). Quantiles are bucket
midpoints clamped to the exact min/max.

### 9.3 `internal/tsc`

On Windows `time.Now()` advances in steps of about 0.5 ms (measured: in a
tight loop of one million pairs of calls only 17 pairs differed). Per-op
latencies of 100-1000 ns cannot be measured with it. `tsc.Ticks()` reads
the invariant time-stamp counter with `RDTSCP` (Go assembly, 17 ns per call
measured), and `NsPerTick` is calibrated once against the wall clock over
200 ms (0.3135 ns/tick, i.e. 3.19 GHz, on the test machine).

## 10. Benchmark methodology (`cmd/goarena-bench`)

* Implementations: `gomap` (256 x RWMutex + `map[string][]byte`), the same
  with `debug.SetGCPercent(400)`, `syncmap` (`sync.Map`), `gobytes` (256
  shards, pointer-free `map[uint64]uint64` index into 256 KiB `[]byte`
  chunks - the bigcache/freecache technique, entirely in the Go heap,
  same-size overwrites in place) and `goarena` (`store.Store`).
* Fairness: same keys (`"key:" + 16 hex digits`, 20 B, bijective scramble of
  the index), same value bytes (128 B default), same seed, same 256 shards,
  same goroutines, same read/write mix, same warm-up and duration. Every
  implementation copies the value on `Set` and copies it into a reused
  per-goroutine buffer on `Get` (`goarena` uses `GetInto`; `gomap` and
  `syncmap` read the stored slice and append it to the buffer outside of
  the lock, which is safe because their values are immutable). Hashing uses
  `maphash` everywhere. No implementation is pre-sized.
* Isolation: the `matrix` driver runs every configuration in a fresh OS
  process (`goarena-bench run`), so heaps, GC state and C allocator state do
  not leak between runs; results are written atomically (temp + rename),
  runs are resumable.
* Phases: populate N keys in parallel (timed), warm-up (not measured),
  measured mixed phase. Every 16th operation is timed with the TSC; the
  timing overhead (~35 ns per sampled op) is identical for all
  implementations. GC metrics are taken as deltas over the measured phase.
* Experiments: E1 (N = 1M, 2M, 5M, 10M, all implementations, 3 repeats,
  15 s + 3 s warm-up), E1b (GOMEMLIMIT = 4 GiB via `debug.SetMemoryLimit`,
  N = 5M and 10M, `gomap`/`gobytes`/`goarena`, 3 repeats, with gctrace), E2
  (goarena chunk size 1..256 MiB, N = 5M), E4 (value size 64..4096 B, N =
  2M), E5 (goroutines 1..16, N = 2M), E6 (Zipf s = 1.1 and 50 % writes, N =
  5M), E7 (GC mark-phase wall time vs N from `GODEBUG=gctrace=1`, parsed by
  the aggregator from per-run stderr side files delimited by `#phase`
  markers; after the measured phase every run also times one forced, blocking
  `runtime.GC()` with the TSC - the cost of a complete GC cycle with the data
  set live). E3 is the `go test -bench` micro-benchmark set.
* Target-domain experiments (v0.2.0): **E8** - large scale, N = 20M, 30M,
  50M for all implementations, 2 repeats, gctrace; a run is executed only if
  its projected private memory (measured private bytes at N = 10M scaled
  linearly, +15 % margin) stays below a 24 GiB budget on the 32 GB machine,
  otherwise the driver records a `.skipped.txt` file with the reason
  instead of risking paging. **E9** - "service with request garbage":
  before every cache operation the worker allocates a request object
  (struct with three pointer fields + a copy of the key + a 1 KiB payload)
  that stays reachable for the next 32 operations of that goroutine
  (`-garbage-rate 1 -garbage-bytes 1024`); identical for every
  implementation; the recorded latency then covers allocation + cache
  operation. N = 5M and 10M, 3 repeats. **E10** - "container memory
  budget": the E9 workload at N = 5M with `debug.SetMemoryLimit` set to 1.25
  x the mean RSS of `gobytes` at N = 5M measured in E1 of the same session,
  for every implementation (goarena's C memory is not counted by the Go
  limit, which is why total RSS is reported for all).

## 11. Example service and dashboard

`examples/httpcache` exposes `PUT/GET/DELETE /kv/{key}`, `/stats`,
`/dashboard`, `/healthz`. A sampler goroutine refreshes the `/stats`
document once per second (ops/s, latency of the last second from a 16-way
sharded histogram, GC CPU % of the last second and since start, GC cycles,
STW time, Go heap gauges, RSS, off-heap bytes, store statistics). The
dashboard is one embedded HTML file (`go:embed`) with hand-written canvas
charts - no CDN, no external resources. `-selfload` starts an in-process
load generator (direct calls plus a trickle of real HTTP requests);
`-backend gomap` swaps the store for a `map[string][]byte` cache so both can
be compared on the same dashboard.

## 12. Deviations from the specification

1. **Shared arena per store** instead of an arena per shard (4.4), to avoid
   reserving 256 x 64 MiB. Shards still own their Slab, lock and index.
2. **Slab spans** (64 KiB) between the arena and the size classes (5.3), to
   keep the shared arena's mutex off the hot path; mid-size classes go to
   the arena directly.
3. **Same-size rehash.** The index grows x2 when `(live+tombstones)/cap >
   0.75` *and* live entries exceed 3/8 of the capacity; when the threshold
   is exceeded mostly because of tombstones, the table is rebuilt at the
   same size. The load factor bound of 0.75 is never violated.
4. **Large-object path through arena dedicated chunks** rather than raw
   `cmem.Malloc` pointers, so that large objects also have a `Ref`.
5. **`store.Config.MaxMemory`** limits all arena memory; since v0.2.0 this
   includes the index tables (v0.1.0: entry memory only).
6. **`Close` returns `nil` on repeated calls**; reads after `Close` report
   "not found" because `Get`/`Has`/`View` have no error result; writes and
   allocation return `ErrClosed`.
7. **Additional packages** `internal/tsc` and `internal/latency` (needed for
   correct latency measurement on Windows), additional experiments E1b, E6
   and E7, and a `gomap` backend in the example.
9. **v0.2.0 departures from the original parameter list** (all motivated by
   measurements, see section 14): hybrid size classes (8-byte steps up to
   256 B, then x1.25; 56 classes instead of the purely geometric 36; in
   v0.3.0 a log-linear table with 201 classes, section 5.1); an
   8-byte entry header without TTL (16 bytes with TTL; key length uint16,
   hence `MaxKeySize` = 65 535); index slot arrays allocated from the
   store's arena instead of directly with `calloc`, and recycled on growth;
   a seqlock (per-shard sequence number) plus a spinning writer lock
   instead of `sync.RWMutex`; large objects retained on large-class free
   lists inside the store.
10. **Target-domain experiments E8-E10** (large N, request garbage,
   container memory budget) were added after the original matrix.
8. **E4** compares `gomap`, `gobytes` and `goarena` only: with 4 KiB values
   and N = 2M the Go-heap variants hold ~8.5 GiB live, and `gomap-gogc400`
   (heap goal 5x live) would exceed the 32 GiB of the test machine.

## 13. Toolchain notes

### 13.1 Coverage counters and large zero-initialised globals

With `-cover` on windows/amd64 (cgo, external linking with MinGW-w64 ld
14.2), declaring the 8 193-byte class lookup table as a global array
(`var classOf8 [8193]uint8`, placed in `.noptrbss`) made the coverage
runtime fail at exit with `slice bounds out of range [:16777222] with
capacity 420` while writing counters - even with zero tests executed.
Bisecting showed that the global array alone triggers it; allocating the
table with `new` at package initialisation removes the problem and has no
measurable cost. The likely cause is section placement of the coverage
counter symbols by the external linker; it does not affect non-coverage
builds, but the workaround is kept so that `go test -cover` is reliable.

### 13.2 Timer resolution on Windows

`time.Now()`/`nanotime` resolution is ~0.5 ms on the test machine (9.3).
Consequences: (1) per-op latency is measured with the TSC; (2) the GC pause
figures reported by the Go runtime itself (MemStats, the pause histogram,
gctrace) are quantised to that resolution - individual sub-millisecond STW
pauses are reported as either 0 or ~0.5 ms. Totals over many pauses remain
unbiased estimates; maxima are upper bounds. The thesis therefore reports
the latency impact of GC mainly through the TSC-measured p99.9/max
operation latencies.

### 13.3 Race detector with cgo on Windows

`go test -race` works with cgo on windows/amd64 with GCC 14.2 (MinGW-w64);
the whole test suite, including the stress tests and the fuzz seed corpus,
is run under the race detector (log in `results/logs/race.txt`). Stress
tests shorten their duration when `-race` is on (build tag `race`).

### 13.4 Fuzzing

`go test -fuzz FuzzStore` works on Windows; the fuzz engine runs 16 worker
processes. The leak check inside the fuzz target only fails on an increase
of live C blocks, because other stores of the same process may be finalized
concurrently.

## 14. Optimisation round v0.1.0 -> v0.2.0 (measured effects)

The v0.1.0 measurements (preserved in `results/v1/`) showed two weak
spots of goarena: it needed 4-12 % more memory than the pointer-free Go-heap
baseline `gobytes`, and under a skewed (Zipf) key distribution its p99.9
latency (116 us) was 20x worse than `sync.Map` (5.6 us). The v0.2.0 round
changed only goarena (the baselines are byte-for-byte the same code) and
re-ran the complete matrix with the v0.1.0 parameters. Changes, in order of
measured impact:

1. **Seqlock reads + `sync.Mutex` writer lock** (7.6, 8.3): readers of a hot
   shard no longer serialise on the RWMutex reader counter. Zipf 5M:
   17.5 -> 48.7 Mops/s, p99.9 116 -> 1.0 us (sync.Map: 22.1 Mops/s, 5.4 us);
   uniform 10M: p99.9 19.7 -> 2.4 us; 50 % writes: 22.7 -> 33.1 Mops/s.
2. **Hybrid size classes + 8-byte header** (5.1, 7.4): the 20 B + 128 B
   entry shrinks from a 192-byte to a 160-byte block; internal
   fragmentation 14.6 % -> 2.5 %; RSS at 10M 2103 -> 1796 MiB (-14.6 %),
   now 5.6 % *below* gobytes (1902 MiB). With 1 KiB values the hybrid table
   is unlucky (1052-byte entries fall into the 1240-byte class, 15.2 % waste
   vs 10.5 % in v0.1.0, RSS +5.3 %).
3. **Index tables in the arena, recycled on growth** (7.3): removes the
   per-table `calloc`/`free` (cgo calls during populate: 5 149 -> 28 at 10M)
   and reuses the outgrown tables (255 MiB at 10M) as entry memory.
4. **Shard layout on cache lines** (7.1) and removal of `defer` from `Has`
   and `GetString`.
5. Profiling (`go tool pprof`, 5M uniform run) shows that 69 % of CPU time
   is now spent in `readOptimistic` itself, almost entirely waiting for the
   two cache misses (slot, entry); hashing takes ~2 %, the harness' key
   formatting ~4 %. The remaining cost is memory latency, not overhead.

Measured regressions and limits (also in `results/SUMMARY.md`):

* **Extreme tail:** goarena v0.2.0 never blocks on the read path, so with 16
  goroutines on 16 logical CPUs the process keeps ~15 CPUs busy and the OS
  occasionally preempts a thread for a scheduler quantum: 0.7-12 sampled
  operations per million exceed 1 ms (v0.1.0: 0; gobytes: 0-1; gomap:
  0.7-20), and the maximum observed latency rose from 0.5-2 ms to 7-24 ms.
  p99.9 improved 7-8x at the same time. With 14 goroutines the effect almost
  disappears (2 operations > 1 ms, max 1.2 ms).
* **Large values:** for 4 KiB values goarena still needs 12.8 % more RSS than
  gobytes (13.2 % class waste).
* **Request garbage (E9/E10, see SUMMARY):** with the default GOGC = 100 the
  Go heap of a goarena process is tiny (3-18 MiB), so the allocation of
  request objects triggers a GC cycle every few MiB (358-412 cycles/s
  versus 6-55 for gobytes, whose 1-3 GiB pointer-free heap acts as GC
  headroom). Each cycle is cheap but frequent; throughput falls to half of
  gobytes'. Raising GOGC to 400 (`goarena-gogc400`) recovers 63 %, and the
  configuration the design actually calls for - GOGC=off with
  GOMEMLIMIT = budget - off-heap bytes (`Store.GoMemoryBudget`,
  `goarena-budget`) - reaches 8.6 Mops/s versus 9.6 for gobytes within the
  same 1 210 MiB container budget, with lower p99.9 (88 vs 99 us) and lower
  RSS (1 029 vs 1 167 MiB).

A spinning writer lock was implemented and measured (7.6) but not adopted.
Exploratory runs of this round are kept in `results/optimization_runs/`.

## 15. GC pacing for off-heap data (`gcpace`, v0.3.0)

### 15.1 Problem

The Go pacer starts a cycle when the heap reaches
`goal = live * (1 + GOGC/100)` (plus stacks and globals). Moving the data set
off-heap is exactly what makes `live` small: in E9 (every operation
allocates a 1 KiB request object) goarena's live Go heap was 3-18 MiB, so the
goal was a few MiB and v0.2.0 ran **358-378 GC cycles per second** (gobytes:
6-11, its 1.8-3.2 GiB pointer-free heap acting as headroom). Each cycle
marked almost nothing, but the fixed costs - two stop-the-world phases,
enabling write barriers, mark assists of allocating goroutines, sweeping -
cut throughput in half (4.73 vs 9.45 Mops/s). This is a pacing problem, not
a marking problem.

### 15.2 Compensation

The pacer makes the runtime behave as if the off-heap bytes `off` were part
of the heap. Requiring the goal of a heap of `live + off` bytes,

    goal = live + (live + off) * G/100   (G = the user's GOGC, 100 by default)

and expressing it through the runtime's own formula
`goal = live * (1 + p/100)` gives the percent to set:

    p = G * (live + off) / live

clamped to [G, 1 000 000], with `live` taken from `/gc/heap/live:bytes`
(raised to the runtime's 4 MiB minimum). Every 50 ms (and right after a
source is registered) the pacer sums the off-heap sources, recomputes `p`
and calls `debug.SetGCPercent` if `p` changed by more than 2 %
(`SetGCPercent` takes the heap lock but does not stop the world).
Consequences: the GC frequency equals that of a heap-resident store of the
same size; each cycle still only marks the small Go heap; peak RSS grows by
up to `(live + off) * G/100` of garbage - the same headroom the heap-resident
store would use.

Memory limits: GOMEMLIMIT counts only memory managed by the Go runtime. If
a total budget is configured (`gcpace.Config.MemoryLimit`), or a GOMEMLIMIT
was in effect when the pacer started (it is then interpreted as the budget
for Go + off-heap memory), the pacer sets the Go limit to
`max(budget - off, 64 MiB)`. With a limit the soft limit, not GOGC, usually
decides when to collect - the configuration that `goarena-budget` applied
by hand in v0.2.0, now automatic and updated as the store grows.

### 15.3 API and lifetime

* `gcpace.Start(Config) *Pacer`, `(*Pacer).Add(src func() int64) (remove)`,
  `Update`, `Stats`, `Stop` (restores the GOGC and GOMEMLIMIT captured by
  `Start`; idempotent).
* Process-wide default pacer: `gcpace.Register(src)` starts it on the first
  registration (configuration from `gcpace.SetDefaultConfig`, by default
  "inherit the current GOGC/GOMEMLIMIT"), sums all sources, and stops it -
  restoring the settings - when the last source unregisters. Multiple paced
  stores therefore cooperate instead of fighting over the global knobs.
* `store.Config.GCPacing = true` registers `arena.ReservedBytes` (not the
  store itself, so a forgotten store can still be finalized) and
  unregisters in `Close` before the memory is released.
* If GOGC was `off` when the pacer started, the percent is left alone and
  only the limit is managed.

### 15.4 Why not alternatives

* *GOGC=off + GOMEMLIMIT only* (`goarena-budget`, v0.2.0): needs a memory
  budget, which batch programs and many services do not have; without it
  the heap would grow unbounded.
* *Fixed high GOGC* (`goarena-gogc400`): helps (7.70 Mops/s in v0.2.0 E9)
  but the right value depends on the ratio off/live, which changes as the
  store fills.
* *Memory ballast* (a large never-touched Go allocation): the classic trick
  raises the goal the same way, but costs a real heap allocation, shows up
  in heap profiles and does not follow the store's growth.
* *Counting C memory inside the runtime*: impossible without modifying the
  runtime.

### 15.5 Verification

Unit tests check the formula (including the E9 case: 16 MiB live + 960 MiB
off-heap -> p = 6100, goal = live + live + off within 1 %), the limit
computation and its floor, that negligible changes do not touch the runtime,
inheritance of GOGC=200 and of a 2 GiB limit, that GOGC=off is left alone,
reference counting of the default pacer, restoration on `Stop`/`Close`, and
an end-to-end run in which 320 MiB of garbage caused 105 GC cycles without
and 1 cycle with 256 MiB of registered off-heap memory. The effect on E9 and
E10 is reported in `results/SUMMARY.md`.

## 16. Iteration v0.2.0 -> v0.3.0 (measured effects)

Re-run in the v0.3.0 session with the v0.2.0 parameters: E1 at N = 10M
(`E1r`), E4, E9, E10, plus `E1t` (tail check). The other experiments use
code paths that did not change (their 156-byte entries keep the 160-byte
class, pacing is off by default) and are reported from the v0.2.0 session.

* **GC pacing (section 15)** - E9 (request garbage, no limit): goarena with
  `GCPacing` 10.05 / 9.94 Mops/s at N = 5M / 10M versus 5.03 / 5.19 with
  default settings and 9.55 / 9.92 for gobytes; GC cycles/s 11.3 / 5.7
  (gobytes 11.2 / 5.9, default goarena 306 / 331); GC CPU 0.40 / 0.25 %;
  p99.9 66 / 55 µs (gobytes 98 / 77 µs). The pacer settled at GOGC = 24 100
  and 46 500. Price: the heap now holds the garbage a heap-resident store
  would hold - RSS 2017 / 3861 MiB, the same as gobytes (1973 / 3840).
  E10 (budget 1210 MiB): 9.17 Mops/s versus 9.52 for gobytes (-3.7 %), RSS
  1132 vs 1159 MiB, p99.9 82 vs 105 µs; the pacer set the Go limit to
  250 MiB (= 1210 - 960 off-heap), matching the hand-tuned `goarena-budget`
  (9.09 Mops/s) automatically.
* **Log-linear size classes (section 5.1)** - E4 RSS versus gobytes: 64 B
  -12.5 %, 256 B -6.5 %, 1 KiB -1.4 % (v0.2.0: +16.6 %), 4 KiB +4.2 % (v0.2.0:
  +12.8 %); internal fragmentation 4.2 / 1.4 / 0.4 / 2.4 %. The remaining
  4 KiB gap is the 2.4 % class rounding plus the index and slab bookkeeping
  (gobytes stores entries packed back to back).
* **No regression at N = 10M** - E1r: goarena 32.37 ± 1.75 Mops/s, RSS
  1797 MiB (v0.2.0 session: 34.96, 1796 MiB). The whole v0.3.0 session ran
  slower on this laptop (gobytes 25.38 vs 28.68, -11.5 %; goarena -7.4 %);
  the ratio goarena/gobytes is 1.28 (v0.2.0: 1.22). p99.9 4.36 µs (v0.2.0
  2.39 µs), still 4-7x below every baseline.
* **Tail vs CPU oversubscription (E1t)** - operations slower than 1 ms per
  million samples, goarena: 10.6 with 16 workers, 0.78 with 15, 0.73 with
  14 (gobytes 0.32 / 0 / 0; gomap 4.2 / 4.6 / 4.9 - its outliers are GC
  related and do not depend on the worker count). The mean per-run maximum
  of goarena fell from 20.0 ms (16) to 4.1 ms (15), but one run with 14
  workers still had a 19 ms outlier. Conclusion: most of goarena's > 1 ms
  outliers are caused by 16 never-blocking goroutines on 16 hardware
  threads; a small residue remains that gobytes does not show. A plausible
  but unverified mechanism is a writer preempted inside its critical
  section (sequence number odd), which makes the readers of that shard spin
  and then fall back to the lock.
