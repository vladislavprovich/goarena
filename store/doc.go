// Package store is goarena's concurrent key-value store: a cache for
// millions of entries whose data lives outside of the Go heap, so that the
// garbage collector's work does not grow with the size of the cache.
//
// All entry data and the hash index live in memory obtained from C through
// cgo; the Go heap holds only a small, fixed amount of bookkeeping (one
// struct per shard). The garbage collector therefore has (almost) nothing to
// mark no matter how many entries the store holds: the cost of a GC cycle
// stops growing with the size of the cache, and so do the GC CPU share and
// the latency impact on the rest of the program.
//
// # Quick start
//
//	s, err := store.New(store.Config{}) // the zero Config is valid
//	if err != nil {
//		return err
//	}
//	defer s.Close() // off-heap memory is freed explicitly, not by the GC
//
//	_ = s.Set([]byte("user:42"), []byte(`{"name":"Ada"}`))
//	v, ok := s.Get([]byte("user:42")) // a copy owned by the caller
//
// Keys and values are byte slices (string variants exist for the common
// operations); they are copied into the store, so the caller may reuse
// them immediately. A Store is safe for concurrent use by any number of
// goroutines.
//
// # Choosing a read method
//
//   - [Store.Get] and [Store.GetString] return a fresh copy (one allocation).
//   - [Store.GetInto] appends the value to a caller-owned buffer and does not
//     allocate when the buffer is large enough: the method of choice on hot
//     paths.
//   - [Store.View] passes a zero-copy view of the value to a callback, under
//     the shard lock; the view must not escape the callback.
//   - [Store.Has] and [Store.HasString] only check presence.
//
// Get, GetInto, GetString and Has take no lock (see "Reads without locks"
// below); View and Range hold the shard lock while the callback runs.
//
// # Writing, TTL and cache-aside
//
// [Store.Set] stores or replaces a value; [Store.SetWithTTL] gives it a time
// to live, and Config.DefaultTTL applies one to every Set. Expired entries
// are invisible to readers at once; [Store.EvictExpired] frees their memory
// and should be called periodically when TTLs are used. [Store.GetOrSet]
// stores a value only if the key is absent (atomically), and
// [Store.GetOrLoad] implements the cache-aside pattern: on a miss it calls a
// loader function without holding any lock and caches the result.
//
// Write errors wrap the sentinel errors [ErrTooLarge], [ErrNoMemory] and
// [ErrClosed] with details; test for them with errors.Is.
//
// # Configuration
//
// The zero [Config] is a good default: 256 shards, 64 MiB arena chunks, no
// memory limit, no TTL. Set Config.MaxMemory to bound the off-heap memory,
// Config.InitialCapacity to pre-size the index when the number of entries is
// known, and Config.GCPacing for services that allocate per request (see
// [Config]). docs/GUIDE.md in the repository has a reference table and
// sizing advice.
//
// # Eviction
//
// By default a full store keeps everything: a write that would exceed
// Config.MaxMemory fails with ErrNoMemory and the old value stays. With
// Config.Eviction = [EvictClock] the store behaves as a bounded cache
// instead. Every index slot then has a reference bit; reads (Get, GetInto,
// GetString, Has, View, GetOrSet/GetOrLoad hits) and overwrites set it - a
// reader writes the bit only when it is clear, so hot keys do not keep
// writing to shared memory. When a write cannot get memory, the CLOCK hand
// of the key's shard sweeps that shard's index: a set bit is cleared and
// the entry skipped (second chance); an entry whose bit is clear is
// evicted if its memory can hold the new entry - preferably one with the
// same block size, otherwise the smallest larger one seen, whose block is
// turned into carving space. Expired entries are removed whenever the
// hand reaches them. If the index itself cannot grow, unreferenced entries
// are evicted and the index is compacted in place.
//
// Semantics, precisely:
//
//   - Eviction happens only inside a write that needs memory, only in the
//     shard of the written key, and never evicts the entry being written
//     or overwritten. A successful write is immediately visible.
//   - Approximation of LRU per shard; entries read since the hand last
//     passed survive the next sweep.
//   - If no entry of the shard can make room (for example a value larger
//     than every entry of the shard), the write fails with ErrNoMemory as
//     without eviction. With values of very different sizes a small share
//     of writes may fail this way.
//   - Evicted entries are counted in Stats.Evictions; lock-free reads stay
//     lock-free and validated by the seqlock, so a reader never sees a
//     torn or evicted value.
//
// # Builds without cgo
//
// With CGO_ENABLED=0 the store works unchanged, but its memory is a set of
// pointer-free blocks in the Go heap instead of C memory (arena.OffHeap is
// false): the collector still does not scan the data, but it counts towards
// GOGC's heap goal and GOMEMLIMIT. Stats.OffHeapBytes is then 0 and
// Config.GCPacing has no effect. Use cgo builds in production; the fallback
// exists so that programs build everywhere.
//
// # Design
//
// A Store is split into shards (256 by default). A key is hashed once with
// hash/maphash (per-store random seed); the top bits of the hash select the
// shard, the low bits the home slot in the shard's index. Every shard has:
//
//   - a sequence number (seqlock) and a writer lock (sync.Mutex);
//   - an open-addressing hash index with linear probing whose slot array
//     lives in the store's arena. A slot is {hash uint64, ref uint64}
//     (16 bytes); hash 0 marks an empty slot and hash 1 a tombstone. The
//     table doubles when (live + tombstones) / capacity would exceed 0.75;
//     the outgrown table is recycled as entry memory;
//   - an [arena.Slab] that allocates entries from 201 size classes, sharing
//     one [arena.Arena] (64 MiB chunks by default) with the other shards.
//
// An entry is stored contiguously in slab memory as
//
//	[keyLen uint16][flags uint16][valLen uint32][expireAt int64, only with TTL][key][value]
//
// # Reads without locks
//
// Get, GetInto, GetString and Has normally take no lock at all: they read
// the shard's sequence number, probe the index, copy the value and check
// that the sequence number has not changed (writers make it odd while they
// mutate the shard). Inconsistent snapshots are retried and, after a few
// attempts, the reader falls back to the shard lock. Readers of a hot key
// therefore never write to a shared cache line. Arena memory is never
// unmapped while the store is open, which is what makes the speculative
// reads safe; Close waits for in-flight readers before it frees memory.
//
// # Safety rules
//
//   - Get, GetInto and GetString return copies; they are always safe.
//   - View and Range pass slices that alias off-heap memory and are valid
//     only during the callback, while the shard lock is held. The callback
//     must not retain them or call back into the store.
//   - Close releases all memory. It is idempotent; after Close writes
//     return ErrClosed and reads report "not found". A finalizer closes
//     stores that become unreachable without Close and counts them in
//     FinalizedStores, but relying on it is a bug: always call Close.
//   - Keys and values are copied into the store; no Go pointer is ever
//     written into C memory.
package store
