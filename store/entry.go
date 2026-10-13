package store

import "unsafe"

// Entry header sizes and flags.
const (
	// entryHeaderSize is the size of the fixed entry header.
	entryHeaderSize = 8
	// ttlFieldSize is the size of the optional expiration field.
	ttlFieldSize = 8
	// flagTTL marks entries that carry an expiration time.
	flagTTL uint16 = 1
)

// Entries are stored contiguously in slab memory:
//
//	offset 0   uint16  key length in bytes (<= 65535)
//	offset 2   uint16  flags (bit 0: the entry has a TTL)
//	offset 4   uint32  value length in bytes
//	offset 8   int64   expiration time, Unix nanoseconds - only if flagTTL
//	offset 8 or 16     key bytes, followed by the value bytes
//
// Entries without a TTL therefore carry an 8-byte header, entries with a
// TTL a 16-byte header. Slab blocks are 8-byte aligned, so every field is
// naturally aligned.

// entrySize returns the number of bytes an entry occupies.
func entrySize(keyLen, valLen int, ttl bool) int {
	n := entryHeaderSize + keyLen + valLen
	if ttl {
		n += ttlFieldSize
	}
	return n
}

// entry is a short-lived view of an entry in slab memory. Writers and
// RLock readers use it under the shard lock; optimistic readers use it only
// after bounds-checking the whole entry and validate what they read with
// the shard's sequence number.
type entry struct {
	p unsafe.Pointer
}

func (e entry) keyLen() int    { return int(*(*uint16)(e.p)) }
func (e entry) flags() uint16  { return *(*uint16)(unsafe.Add(e.p, 2)) }
func (e entry) valLen() int    { return int(*(*uint32)(unsafe.Add(e.p, 4))) }
func (e entry) hasTTL() bool   { return e.flags()&flagTTL != 0 }
func (e entry) headerLen() int { return entryHeaderSize + int(e.flags()&flagTTL)*ttlFieldSize }

// expireAt returns the expiration time, or 0 if the entry has no TTL.
func (e entry) expireAt() int64 {
	if !e.hasTTL() {
		return 0
	}
	return *(*int64)(unsafe.Add(e.p, entryHeaderSize))
}

func (e entry) size() int { return e.headerLen() + e.keyLen() + e.valLen() }

func (e entry) key() []byte {
	return unsafe.Slice((*byte)(unsafe.Add(e.p, e.headerLen())), e.keyLen())
}

func (e entry) value() []byte {
	return unsafe.Slice((*byte)(unsafe.Add(e.p, e.headerLen()+e.keyLen())), e.valLen())
}

// expired reports whether the entry has a TTL that elapsed at time now.
func (e entry) expired(now int64) bool {
	x := e.expireAt()
	return x != 0 && now >= x
}

// write fills the entry memory (which must hold entrySize bytes).
func (e entry) write(key, val []byte, expireAt int64) {
	*(*uint16)(e.p) = uint16(len(key))
	*(*uint32)(unsafe.Add(e.p, 4)) = uint32(len(val))
	if expireAt != 0 {
		*(*uint16)(unsafe.Add(e.p, 2)) = flagTTL
		*(*int64)(unsafe.Add(e.p, entryHeaderSize)) = expireAt
	} else {
		*(*uint16)(unsafe.Add(e.p, 2)) = 0
	}
	copy(e.key(), key)
	copy(e.value(), val)
}

// keyEqual compares the stored key with k.
func (e entry) keyEqual(k []byte) bool {
	if e.keyLen() != len(k) {
		return false
	}
	return string(e.key()) == string(k)
}
