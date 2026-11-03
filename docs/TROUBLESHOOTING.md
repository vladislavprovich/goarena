# Troubleshooting

Symptoms, causes and fixes for problems with building and running goarena.
General questions are answered in the [FAQ](FAQ.md).

## Contents

- Building
  - [cgo is not enabled](#cgo-is-not-enabled)
  - [gcc not found on Windows](#gcc-not-found-on-windows)
  - [C compiler not found on macOS](#c-compiler-not-found-on-macos)
  - [gcc not found on Linux and in Docker](#gcc-not-found-on-linux-and-in-docker)
  - [Cross-compilation](#cross-compilation)
  - [undefined: cmem.Malloc](#undefined-cmemmalloc)
- Running
  - [The program runs in CGO_ENABLED=0 mode](#the-program-runs-in-cgo_enabled0-mode)
  - [Memory is not returned to the OS until Close](#memory-is-not-returned-to-the-os-until-close)
  - [RSS is much larger than the Go heap](#rss-is-much-larger-than-the-go-heap)
  - [Writes fail with ErrNoMemory](#writes-fail-with-errnomemory)
  - [The container is OOM-killed despite GOMEMLIMIT](#the-container-is-oom-killed-despite-gomemlimit)
  - [High number of GC cycles](#high-number-of-gc-cycles)
  - [FinalizedStores is not zero](#finalizedstores-is-not-zero)
  - [Deadlock in a View or Range callback](#deadlock-in-a-view-or-range-callback)
  - [Crash or garbage after Close](#crash-or-garbage-after-close)
- Testing
  - [Race detector and cgo](#race-detector-and-cgo)

First diagnostic for almost every problem:

```sh
go version          # Go 1.27 or newer
go env CGO_ENABLED  # 1 for off-heap memory
go env CC           # the C compiler cgo will run
```

and, in the program, `log.Print(arena.OffHeap, s.Stats())`.

---

## Building

### cgo is not enabled

**Symptom.** `go env CGO_ENABLED` prints `0`, `arena.OffHeap` is `false`,
`Stats.OffHeapBytes` is 0 and `Stats.String()` says "reserved in the Go heap
(built without cgo)".

**Cause.** cgo is off because it was disabled explicitly, because the build
cross-compiles (cgo is off by default then), or because the `go` command found
no C compiler in `PATH` (since Go 1.20 cgo is disabled by default in that
case). goarena then builds in its pure-Go fallback mode
([GUIDE.md](GUIDE.md#builds-without-cgo)).

**Fix.** Install a C compiler (sections below), then:

```sh
go env -w CGO_ENABLED=1   # or export CGO_ENABLED=1 for one shell
go clean -cache           # optional: force a rebuild of cached packages
go build ./...
```

To make a deployment fail loudly instead of silently running without
off-heap memory:

```go
if !arena.OffHeap {
	log.Fatal("goarena was built without cgo; rebuild with CGO_ENABLED=1")
}
```

### gcc not found on Windows

**Symptom.**

```
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
```

or, with a 32-bit MinGW: `sorry, unimplemented: 64-bit mode not compiled in`.

**Fix.** Install a 64-bit **MinGW-w64** GCC and put its `bin` directory first
in `PATH`:

- [WinLibs](https://winlibs.com/) or [MinGW-Builds](https://github.com/niXman/mingw-builds-binaries/releases):
  download the x86_64 (posix, seh) archive, unpack it (for example to
  `C:\mingw64`) and add `C:\mingw64\bin` to `PATH`;
- or [MSYS2](https://www.msys2.org/): `pacman -S mingw-w64-ucrt-x86_64-gcc`,
  then add `C:\msys64\ucrt64\bin` to `PATH`.

Check in a new terminal:

```bat
gcc --version
go env CGO_ENABLED CC
```

goarena is developed and benchmarked with MinGW-w64 GCC 14.2. If several GCCs
are installed (Strawberry Perl, old MinGW, Cygwin), the first one in `PATH`
wins; `where gcc` shows the order, or point cgo at one explicitly with
`go env -w CC=C:\mingw64\bin\gcc.exe`.

### C compiler not found on macOS

**Symptom.** `xcrun: error: invalid active developer path` or
`cgo: C compiler "clang" not found`.

**Fix.** Install the Xcode Command Line Tools (also needed again after some
macOS upgrades):

```sh
xcode-select --install
```

### gcc not found on Linux and in Docker

**Symptom.** `cgo: C compiler "gcc" not found: exec: "gcc": executable file
not found in $PATH`.

**Fix.** Install the distribution's compiler package:

| distribution | command |
|---|---|
| Debian, Ubuntu | `sudo apt install build-essential` |
| Fedora, RHEL | `sudo dnf install gcc` |
| Alpine | `apk add build-base` |
| Arch | `sudo pacman -S base-devel` |

In Docker, the Debian-based `golang:1.27` images include gcc; `golang:1.27-alpine`
does not (`RUN apk add build-base`). A cgo binary is dynamically linked
against the C library of the build image by default, so the runtime image
must provide the same C library: build on Debian and run on
`gcr.io/distroless/base-debian12` or `debian:bookworm-slim`, or build and run
on Alpine (musl). `scratch` and `distroless/static` images have no C library.

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -o /app ./cmd/server

FROM gcr.io/distroless/base-debian12
COPY --from=build /app /app
ENTRYPOINT ["/app"]
```

### Cross-compilation

**Symptom.** `GOOS=linux GOARCH=arm64 go build` succeeds, but the binary runs
without off-heap memory (`arena.OffHeap` is `false`).

**Cause.** When `GOOS`/`GOARCH` differ from the host, cgo is disabled by
default and goarena builds its pure-Go fallback. That build works everywhere,
just without the off-heap benefits.

**Fix.** Use a C cross-compiler for the target:

```sh
# Debian/Ubuntu host, linux/arm64 target
sudo apt install gcc-aarch64-linux-gnu
CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 go build ./...

# with Zig as a portable C cross-compiler
CGO_ENABLED=1 CC="zig cc -target aarch64-linux-gnu" GOOS=linux GOARCH=arm64 go build ./...
```

or build natively for the target platform (a CI runner of that architecture,
or `docker buildx build --platform linux/arm64` with a Go image).

### undefined: cmem.Malloc

**Symptom.** `arena/arena.go: undefined: cmem.Malloc` when building with
`CGO_ENABLED=0`.

**Cause.** goarena versions before 0.4.0 had no pure-Go fallback and did not
build without cgo.

**Fix.** Upgrade (`go get github.com/vladislavprovich/goarena@latest`), or
enable cgo as described above.

---

## Running

### The program runs in CGO_ENABLED=0 mode

See [cgo is not enabled](#cgo-is-not-enabled). In that mode everything works,
but the data counts towards the Go heap: heap profiles show it, `GOMEMLIMIT`
includes it, `Config.GCPacing` has no effect, and an allocation the Go runtime
cannot satisfy terminates the process instead of returning `ErrNoMemory`.

### Memory is not returned to the OS until Close

**Symptom.** After deleting most entries (or after `EvictExpired`) the process
RSS stays high; `Stats.FreeListBytes` is large.

**Cause.** By design, freed blocks are kept on per-size-class free lists for
reuse, and chunks are only freed by `Close`: lock-free readers may still be
reading memory that a writer frees, so it must stay mapped while the store is
open ([DESIGN.md, section 8](DESIGN.md#8-concurrency-and-memory-safety-argument)).
New entries of similar sizes reuse that memory; entries of other size classes
do not.

**Fix.** If the data set shrinks permanently, or value sizes drift, rebuild:
create a new store, copy the live entries with `Range`, swap it in, and
`Close` the old one. Size `MaxMemory` for the peak, not the average.

### RSS is much larger than the Go heap

**Symptom.** `pprof` heap profiles, `runtime.MemStats.HeapAlloc` or the
`go_memstats_*` metrics show a few MiB, while the container uses gigabytes.

**Cause.** That is the point of goarena: the data is C memory, which Go tools
do not see. It is visible in the process RSS and in
`Stats.ArenaReservedBytes` / `Stats.OffHeapBytes`.

**Check.** RSS ≈ Go runtime memory (`gcstats.Read().RuntimeBytes`) +
`OffHeapBytes` + memory of other C libraries and thread stacks. Export
`OffHeapBytes` next to your heap metrics
([GUIDE.md](GUIDE.md#exporting-metrics)). Note that `ArenaReservedBytes` is
reserved memory; pages that were never written may not be resident yet.

### Writes fail with ErrNoMemory

**Symptom.** `Set` returns an error such as

```
store: out of off-heap memory: cannot store a 20-byte key with a 128-byte value:
67108864 of 104857600 bytes (Config.MaxMemory) reserved in chunks of 67108864 bytes;
delete entries, call EvictExpired, raise MaxMemory or lower ChunkSize
```

**Causes and fixes.**

- The store is full: `ArenaReservedBytes` is at `MaxMemory`. Free memory
  (`EvictExpired`, `Delete`), raise the limit, or skip caching when full.
- The limit is not a multiple of `ChunkSize`: in the message above only one
  64 MiB chunk fits into 100 MiB. Use a multiple of the chunk size or a
  smaller `ChunkSize`.
- Memory is stuck on free lists of size classes that are no longer used
  (`FreeListBytes`): see [Memory is not returned to the OS until Close](#memory-is-not-returned-to-the-os-until-close).
- Without `MaxMemory`: the C allocator failed (the system is out of memory).

`errors.Is(err, store.ErrNoMemory)` identifies all of these; the key's old
value is unchanged.

### The container is OOM-killed despite GOMEMLIMIT

**Cause.** `GOMEMLIMIT` limits only Go-managed memory; the store's C memory
comes on top of it. Without `GCPacing` the Go heap may grow to the full limit
while the store holds gigabytes more.

**Fix.** Enable `Config.GCPacing` (it treats `GOMEMLIMIT` as the budget for Go
*plus* off-heap memory), set `GOMEMLIMIT` to about 90 % of the container limit
and `MaxMemory` well below it. Recipe:
[GUIDE.md, Containers and Kubernetes](GUIDE.md#containers-and-kubernetes).

### High number of GC cycles

**Symptom.** `gcstats` or `GODEBUG=gctrace=1` shows hundreds of GC cycles per
second, throughput is lower than expected, the heap goal is a few MiB.

**Cause.** The store moved the data out of the Go heap, so the live heap is
tiny and `GOGC=100` triggers a cycle every few MiB of request garbage. In the
benchmark this cost half the throughput (306 cycles/s, 5.03 Mops/s).

**Fix.** `store.Config{GCPacing: true}` (10.05 Mops/s, 11.3 cycles/s in the
same benchmark). [examples/gc-pacing](../examples/gc-pacing) shows the effect
in a few seconds.

### FinalizedStores is not zero

**Symptom.** `store.FinalizedStores()` returns a positive number.

**Cause.** Stores became unreachable without `Close`; the GC's finalizer
closed them. Until it ran, their memory was leaked.

**Fix.** Find the missing `Close`: typical places are stores created per
request or per test, and error paths that return before a `defer s.Close()`.
In tests use `t.Cleanup(func() { s.Close() })` and the `TestMain` check from
[GUIDE.md](GUIDE.md#testing-code-that-uses-goarena).

### Deadlock in a View or Range callback

**Symptom.** A goroutine hangs inside `View` or `Range`, often together with
writers of the same store.

**Cause.** The callback runs while the shard's lock is held. Calling a write
method (or a read that falls back to the lock) of the same store from the
callback can wait for that lock forever.

**Fix.** Copy what you need inside the callback and act on it afterwards:

```go
var keys [][]byte
s.Range(func(k, v []byte) bool {
	if isStale(v) {
		keys = append(keys, append([]byte(nil), k...)) // copy the key
	}
	return true
})
for _, k := range keys {
	s.Delete(k) // outside of the callback
}
```

### Crash or garbage after Close

**Symptom.** A segmentation fault (`unexpected signal`), or values that
change unexpectedly.

**Cause.** A slice obtained from `View`, `Range`, `Arena.Bytes` or
`Slab.Bytes`, or a pointer from `arena.NewValue`/`NewSlice`, was kept and used
after the memory was freed (`Close`, `Release`, `Reset`, `Delete`,
overwrite). Off-heap memory is not protected by the GC.

**Fix.** Never keep views beyond their callback or the lifetime of the
arena; copy the data (`Get`, `GetInto`, `append([]byte(nil), v...)`). Close
the store only after every user is done with it (for example after
`http.Server.Shutdown` returned).

---

## Testing

### Race detector and cgo

- On most platforms `go test -race` requires cgo (`-race requires cgo;
  enable cgo by setting CGO_ENABLED=1`; macOS is an exception): install a C
  compiler as above.
- With cgo, the race detector does not instrument C memory, so the
  intentional, validated races of the lock-free read path are not reported;
  all Go-memory variables shared with readers are accessed atomically, and the
  whole test suite passes under `-race`.
- In a race build without cgo (possible on macOS), the store's memory is Go
  heap, which the race detector instruments; reads then take the shard lock
  instead of the lock-free path, so no false reports occur.
- On Windows the race detector with cgo depends on the MinGW-w64 version; it
  is verified with GCC 14.2 ([DESIGN.md, 13.3](DESIGN.md#133-race-detector-with-cgo-on-windows)).
  CI runs `-race` on Linux and macOS.
