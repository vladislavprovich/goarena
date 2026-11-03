# Contributing to goarena

## Setup

goarena needs Go 1.27 or newer and cgo (`CGO_ENABLED=1` plus a C compiler in
`PATH`: gcc or clang on Linux/macOS, MinGW-w64 GCC on Windows; see
[TROUBLESHOOTING.md](TROUBLESHOOTING.md#cgo-is-not-enabled)). The module
uses only the standard library. It must also build and pass its tests
without cgo (`CGO_ENABLED=0`), where `internal/cmem` falls back to Go-heap
blocks.

## Workflow

```sh
make fmt      # gofmt check (CI fails on unformatted files)
make vet      # go vet ./...
make test     # go test -count=1 ./...
make race     # go test -race -count=1 ./...
make nocgo    # vet, build and test with CGO_ENABLED=0 (pure-Go fallback)
make examples # run the short examples
make build    # compile everything; harness and examples go to bin/
```

CI (`.github/workflows/ci.yml`) runs the same steps on Linux, Windows and
macOS, plus a Linux job with `CGO_ENABLED=0`; please make sure they pass
locally before opening a pull request.

## Code layout rules

* One directory is one package. Public packages are `arena`, `store`,
  `gcpace` and `gcstats`; everything else lives under `internal/`
  (libraries), `cmd/` (tools) or `examples/` (runnable programs). There is
  no Go code in the repository root.
* A file holds one responsibility and its name says which
  (`store_write_operations.go`, `slab_free_lists.go`, ...). Keep files below
  roughly 400 lines when there is a natural split.
* Tests sit next to the code they test: `x.go` is tested in `x_test.go`.
  Runnable documentation examples go into `example_test.go`, the package
  documentation into `doc.go`.
* Platform-specific code uses file-name suffixes (`_linux.go`,
  `_windows.go`, `_amd64.go`/`.s`) or `//go:build` constraints, with a
  portable `_other.go` fallback.
* Only `internal/cmem` may import `"C"`, and only in files built with cgo
  (`c_allocator.go`); `go_heap_allocator.go` is its `!cgo` counterpart and
  must keep the same API. Never store Go pointers in memory obtained from it
  (see the safety rules in the README).
* Tests that check off-heap specifics (C memory, cgo call counts, the size
  of the Go heap) must depend on `arena.OffHeap` / `cmem.OffHeap` so that
  they pass in both modes.

## Changes

* Keep the public API stable; compare `go doc -all ./<package>` before and
  after a change. Every exported identifier needs a doc comment, and main
  entry points a runnable `Example` in `example_test.go`.
* User documentation lives in `docs/` (index: [docs/README.md](README.md));
  update [GUIDE.md](GUIDE.md) and the README when behaviour or the API
  changes, and quote numbers only from [BENCHMARKS.md](BENCHMARKS.md).
* Add a test for every bug fix and every new behaviour; performance claims
  need a benchmark (`cmd/goarena-bench` or a `Benchmark*` function) and an
  entry in `docs/BENCHMARKS.md`.
* Record user-visible changes under a new version heading in `CHANGELOG.md`
  and bump `internal/version`.
