# Contributing to goarena

## Setup

goarena needs Go 1.27 or newer and cgo (`CGO_ENABLED=1` plus a C compiler in
`PATH`: gcc or clang on Linux/macOS, MinGW-w64 GCC on Windows). The module
uses only the standard library.

## Workflow

```sh
make fmt      # gofmt check (CI fails on unformatted files)
make vet      # go vet ./...
make test     # go test -count=1 ./...
make race     # go test -race -count=1 ./...
make examples # run the short examples
make build    # compile everything; harness and examples go to bin/
```

CI (`.github/workflows/ci.yml`) runs the same steps on Linux, Windows and
macOS; please make sure they pass locally before opening a pull request.

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
* Only `internal/cmem` may import `"C"`. Never store Go pointers in memory
  obtained from it (see the safety rules in the README).

## Changes

* Keep the public API stable; compare `go doc -all ./<package>` before and
  after a change.
* Add a test for every bug fix and every new behaviour; performance claims
  need a benchmark (`cmd/goarena-bench` or a `Benchmark*` function) and an
  entry in `docs/BENCHMARKS.md`.
* Record user-visible changes under a new version heading in `CHANGELOG.md`
  and bump `internal/version`.
