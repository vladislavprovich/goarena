//go:build !(amd64 || 386)

package store

import "sync/atomic"

// readFence orders the plain loads an optimistic reader made before it
// against the atomic sequence-number load that validates them (the
// smp_rmb of a seqlock reader).
//
// On weakly ordered processors (arm64, ppc64, riscv64, loong64, ...) an
// atomic load only has acquire semantics: it keeps later accesses after
// it, but earlier plain loads may still be satisfied after it. A reader
// could then validate an unchanged sequence number while having copied
// bytes of a concurrent overwrite. Go has no standalone fence, so a
// sequentially consistent read-modify-write on a goroutine-local word is
// used instead: its release half orders every earlier load before it, and
// the following atomic load of the sequence number cannot be reordered
// before it. The word lives on the reader's stack, so readers of a hot key
// do not contend on a shared cache line.
func readFence() {
	var w atomic.Uint32
	w.Add(1)
}
