//go:build amd64 || 386

package store

// readFence orders the plain loads an optimistic reader made before it
// against the atomic sequence-number load that validates them (the
// smp_rmb of a seqlock reader).
//
// On amd64 and 386 the processor never reorders a load with an earlier
// load (total store order), and the Go compiler does not move memory
// accesses across an atomic load, so no instruction is needed.
func readFence() {}
