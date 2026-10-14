# Frequently asked questions

Short answers with links to the details. Problems with building or running
goarena are covered in [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## Contents

- General
  - [What is goarena, in one sentence?](#what-is-goarena-in-one-sentence)
  - [Is it a database?](#is-it-a-database)
  - [How does it compare with bigcache and freecache?](#how-does-it-compare-with-bigcache-and-freecache)
  - [Which Go versions and platforms are supported?](#which-go-versions-and-platforms-are-supported)
  - [Does it work without cgo?](#does-it-work-without-cgo)
- Using the store
  - [Can I store Go structs?](#can-i-store-go-structs)
  - [Why are values copied?](#why-are-values-copied)
  - [Get, GetInto or View - which one should I use?](#get-getinto-or-view---which-one-should-i-use)
  - [Does the store evict entries when it is full?](#does-the-store-evict-entries-when-it-is-full)
  - [Do expired entries free their memory automatically?](#do-expired-entries-free-their-memory-automatically)
  - [Does GetOrLoad prevent duplicate loads?](#does-getorload-prevent-duplicate-loads)
  - [What are the size limits?](#what-are-the-size-limits)
  - [Is iteration ordered? Can I take a snapshot?](#is-iteration-ordered-can-i-take-a-snapshot)
  - [Is it safe for concurrent use?](#is-it-safe-for-concurrent-use)
- Memory and GC
  - [How much memory does an entry need?](#how-much-memory-does-an-entry-need)
  - [Why does memory not shrink after I delete entries?](#why-does-memory-not-shrink-after-i-delete-entries)
  - [Why do heap profiles show almost nothing while RSS is large?](#why-do-heap-profiles-show-almost-nothing-while-rss-is-large)
  - [Does GOMEMLIMIT include the store?](#does-gomemlimit-include-the-store)
  - [When should I enable GCPacing?](#when-should-i-enable-gcpacing)
  - [What happens if I forget Close?](#what-happens-if-i-forget-close)
  - [Can I use many small stores?](#can-i-use-many-small-stores)

---

## General

### What is goarena, in one sentence?

A Go key-value store (and the allocators under it) that keeps large,
long-lived data sets in C memory, so that the garbage collector's work and
the process memory no longer grow with the size of your cache.

### Is it a database?

No. There is no persistence, no transactions, no replication and no
iteration order; everything is gone after `Close`. It is an in-process cache.
See [USE_CASES.md](USE_CASES.md#you-need-persistence-or-transactions) for
alternatives.

### How does it compare with bigcache and freecache?

They avoid GC *scanning* by storing entries in large pointer-free `[]byte`
chunks inside the Go heap, but that memory still counts towards the heap goal
and `GOMEMLIMIT`, and they evict old entries when full. goarena keeps the data
outside of the Go heap, reads without locks and has no eviction besides TTL.
The benchmarks include a bigcache-style baseline (`gobytes`): at 10 M entries
goarena did 32.37 Mops/s (p99.9 4.36 µs, 1797 MiB RSS) versus 25.38 Mops/s
(28.67 µs, 1912 MiB). The baseline wins in a few cases, for example 4 KiB
values need 4.2 % less memory there
([BENCHMARKS.md](BENCHMARKS.md#where-goarena-does-not-win)). A migration table
is in [GUIDE.md](GUIDE.md#from-bigcache-or-freecache).

### Which Go versions and platforms are supported?

Go 1.27 or newer (`go.mod`). CI tests Linux and Windows (amd64) and macOS
(Apple Silicon, arm64) with cgo, plus a Linux build without cgo. The seqlock
read path has an explicit read fence for weakly ordered processors (arm64,
ppc64, riscv64, loong64). Process RSS in `gcstats` is reported on Linux and
Windows only. The module uses only the standard library.

### Does it work without cgo?

Yes. With `CGO_ENABLED=0` goarena uses a pure-Go fallback: the same API and
behaviour, but the data lives in pointer-free Go-heap blocks (`arena.OffHeap`
is `false`), so the GC still does not scan it but counts it towards the heap
goal and `GOMEMLIMIT`. Use cgo in production to get the benefits. Details:
[GUIDE.md](GUIDE.md#builds-without-cgo).

## Using the store

### Can I store Go structs?

Not directly in the store: keys and values are bytes, so serialize them (JSON,
protobuf, `encoding/binary`, gob). Go pointers - including strings, slices,
maps and interfaces inside a struct - must never be written into off-heap
memory, because the GC cannot see them. Structs made only of numbers, bools
and fixed-size arrays can live in an `arena.Arena` via `arena.NewValue` and
`arena.NewSlice` ([GUIDE.md](GUIDE.md#typed-data-with-the-arena-package)).

### Why are values copied?

Because a slice into off-heap memory would dangle as soon as the entry is
overwritten, deleted or the store closed. `Get` and `GetInto` copy, which
makes them always safe; `GetInto` reuses your buffer, so a hot read path does
not allocate. `View` gives zero-copy access, valid only inside its callback.

### Get, GetInto or View - which one should I use?

`GetInto` with a reused buffer on hot paths; `Get` when simplicity matters
more than one allocation; `View` to parse a large value in place without
copying it (the shard's writers wait while the callback runs). The full table
is in [GUIDE.md](GUIDE.md#reading).

### Does the store evict entries when it is full?

No. Besides TTLs goarena never drops entries by itself. When `MaxMemory` is
reached, writes fail with an error that wraps `store.ErrNoMemory`, and the old
values stay intact. Free memory with `Delete` or `EvictExpired`, or use a cache
with an eviction policy ([USE_CASES.md](USE_CASES.md#you-need-an-eviction-policy-lru-lfu)).

### Do expired entries free their memory automatically?

They become invisible to readers immediately, but their memory is only freed
by `EvictExpired`. There is no background goroutine; call it periodically
([GUIDE.md](GUIDE.md#periodic-eviction)). Until then they count in `Len`.

### Does GetOrLoad prevent duplicate loads?

No. On a miss it calls your loader without holding a lock, so goroutines that
miss the same key at the same time may each call it. All of them return the
value that was stored first. Wrap the call in a single-flight group if
duplicate loads are expensive ([GUIDE.md](GUIDE.md#cache-aside-with-getorload)).

### What are the size limits?

Keys up to 65 535 bytes (`store.MaxKeySize`), entries (8- or 16-byte header +
key + value) up to 1 GiB (`store.MaxEntrySize`); larger writes return an error
wrapping `store.ErrTooLarge`. Up to 65 536 shards; chunks from 4 KiB to 2 GiB.

### Is iteration ordered? Can I take a snapshot?

`Range` visits entries in no particular order, one shard at a time, holding
that shard's lock; it is not a snapshot of the whole store. To export the
data, `Range` over it and copy keys and values into your own format.

### Is it safe for concurrent use?

`store.Store` and `arena.Arena` are; `arena.Slab` is not. Every single-key
operation is atomic, reads never block, and `Close` waits for in-flight
operations. The rules for `View`/`Range` callbacks and the full list of
guarantees are in [GUIDE.md](GUIDE.md#concurrency-guarantees).

## Memory and GC

### How much memory does an entry need?

About `class(8 + len(key) + len(value)) + 16 / load factor` bytes: the entry
rounded up to a size class plus its share of the index (21 - 43 bytes). With
20-byte keys and 128-byte values that is about 187 bytes; 10 M such entries
measured 1797 MiB RSS. Formula and table: [GUIDE.md](GUIDE.md#memory-sizing).

### Why does memory not shrink after I delete entries?

Freed blocks go to per-size-class free lists and are reused by later writes of
a similar size (`Stats.FreeListBytes`), but chunks are returned to the
operating system only by `Close`. This is deliberate: lock-free readers may
still be reading a block while it is being freed, so memory must stay mapped
while the store is open. To shrink, build a new store, copy the live entries
(`Range`) and close the old one.

### Why do heap profiles show almost nothing while RSS is large?

Heap profiles, `runtime.MemStats` and `GOMEMLIMIT` only see memory managed by
the Go runtime. The store's data is C memory: it shows up in the process RSS
and in `Stats.OffHeapBytes` / `ArenaReservedBytes`, not in `pprof`. Compare RSS
with Go heap + `OffHeapBytes` ([TROUBLESHOOTING.md](TROUBLESHOOTING.md#rss-is-much-larger-than-the-go-heap)).

### Does GOMEMLIMIT include the store?

No (with cgo). Either enable `Config.GCPacing`, which treats an existing
`GOMEMLIMIT` as the budget for Go *plus* off-heap memory and lowers the Go
limit accordingly, or set the Go limit yourself with
`Store.GoMemoryBudget(total)` ([GUIDE.md](GUIDE.md#gomemlimit-and-memory-budgets)).

### When should I enable GCPacing?

For services that allocate per request (decoding, buffers, responses). With
the data off-heap the Go heap is tiny and the default `GOGC=100` can run
hundreds of GC cycles per second; in the benchmark `GCPacing` doubled
throughput (5.03 to 10.05 Mops/s) and cut p99.9 from 384 to 66 µs, at the price
of more RSS (946 to 2017 MiB). Batch jobs and programs that allocate little do
not need it ([GUIDE.md](GUIDE.md#gc-pacing-and-gomemlimit)).

### What happens if I forget Close?

The memory stays allocated until a finalizer closes the store after the GC
found it unreachable - at an unpredictable time, possibly never before the
program exits. The finalizer increments `store.FinalizedStores()`; a non-zero
value is a leak in your program. Always `defer s.Close()`, and check
`FinalizedStores` in tests ([GUIDE.md](GUIDE.md#testing-code-that-uses-goarena)).

### Can I use many small stores?

Yes, but configure them for it: every store reserves at least one chunk (64
MiB by default, because the initial index lives in the arena) and keeps about
8 KiB of Go heap per shard. For small stores use `ChunkSize: 1 << 20` and
`Shards: 16` (or fewer). For a very small amount of data, a plain map is
usually the better choice ([USE_CASES.md](USE_CASES.md#small-data-sets)).
