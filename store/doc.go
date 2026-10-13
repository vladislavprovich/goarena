// Package store implements an off-heap, concurrent key-value store for Go:
// the experimental prototype of goarena's hybrid memory-management model.
//
// All entry data and the hash index live in memory obtained from C through
// cgo; the Go heap holds only a small, fixed amount of bookkeeping (one
// struct per shard). The garbage collector therefore has (almost) nothing to
// mark no matter how many entries the store holds: the cost of a GC cycle
// stops growing with the size of the cache, and so do the GC CPU share and
// the latency impact on the rest of the program.
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
