package store

import (
	"hash/maphash"
	"sync/atomic"

	"github.com/vladislavprovich/goarena/arena"
)

// shard is one independently locked partition of the store. Its index and
// entry data live in C memory; the struct itself holds only integers, the
// lock, an atomic pointer to the immutable table descriptor and two
// pointers to Go objects. The layout keeps the fields read by lock-free
// readers (first cache line) apart from the writer-only state, and the
// struct size is a multiple of 64 bytes so that shards never share a line.
type shard struct {
	// --- cache line 0: read by optimistic readers
	seq atomic.Uint64              // even: stable; odd: a writer is mutating
	tab atomic.Pointer[indexTable] // current index table
	a   *arena.Arena               // shared arena (chunk table for refs)
	_   [40]byte

	// --- cache lines 1-2: writer state (guarded by mu)
	mu         shardLock // writers, and readers on the slow path
	slab       *arena.Slab
	live       uint64   // slots holding entries
	tombs      uint64   // tombstone slots
	kvBytes    int64    // sum of len(key)+len(value) of live entries
	entryBytes int64    // sum of entry sizes (incl. headers) of live entries
	sets       uint64   // successful Set calls
	deletes    uint64   // successful Delete calls
	expired    uint64   // entries removed because their TTL elapsed
	_          [56]byte // pad the struct to 192 bytes (3 cache lines)
}

// beginWrite and endWrite bracket every mutation of index slots or entry
// memory (sh.mu must be held): the odd sequence number tells optimistic
// readers that what they read may be inconsistent.
func (sh *shard) beginWrite() { sh.seq.Add(1) }

func (sh *shard) endWrite() { sh.seq.Add(1) }

// hash computes the remapped 64-bit hash of key.
func (s *Store) hash(key []byte) uint64 { return fixHash(maphash.Bytes(s.seed, key)) }

// shardFor selects the shard for hash h using its top bits (the index uses
// the low bits, so the two choices are independent).
func (s *Store) shardFor(h uint64) *shard {
	if len(s.shards) == 1 {
		return &s.shards[0]
	}
	return &s.shards[h>>s.shardShift]
}

// entryAt returns a view of the entry referenced by ref (locked paths only).
func (sh *shard) entryAt(ref uint64) entry { return entry{p: sh.a.Pointer(arena.Ref(ref))} }

// find looks key up under the shard lock. It returns the table, the slot
// position and the entry when found.
func (sh *shard) find(h uint64, key []byte) (*indexTable, uint64, entry, bool) {
	t := sh.tab.Load()
	i := h & t.mask
	for {
		sl := t.at(i)
		switch sl.hash {
		case hashEmpty:
			return t, i, entry{}, false
		case h:
			if e := sh.entryAt(sl.ref); e.keyEqual(key) {
				return t, i, e, true
			}
		}
		i = (i + 1) & t.mask
	}
}
