# goarena documentation

Start with the [project README](../README.md) for the overview and a quick
start. The documents below go deeper; pick by what you need.

| document | read it when you want to ... |
|---|---|
| [GUIDE.md](GUIDE.md) | use goarena in a program: concepts, configuration reference, reading and writing, TTL and eviction, memory sizing, GC pacing and `GOMEMLIMIT` in containers, metrics, concurrency guarantees, typed arenas, error handling, testing, migration from `map`/`sync.Map`/bigcache/freecache, production checklist |
| [USE_CASES.md](USE_CASES.md) | decide whether goarena fits your workload: good and poor fits with numbers, alternatives, a decision flowchart |
| [FAQ.md](FAQ.md) | get short answers to common questions |
| [TROUBLESHOOTING.md](TROUBLESHOOTING.md) | fix a problem: cgo and C compiler setup on Windows/macOS/Linux, cross-compilation, memory and GC symptoms, `ErrNoMemory`, leaks, the race detector |
| [DESIGN.md](DESIGN.md) | understand the internals: memory layouts with byte offsets, the allocators, the hash index, the lock-free read path and its safety argument, GC pacing |
| [BENCHMARKS.md](BENCHMARKS.md) | see how goarena was measured, all results, and how to reproduce them |
| [CONTRIBUTING.md](CONTRIBUTING.md) | work on goarena itself: setup, workflow, code layout rules |
| [../CHANGELOG.md](../CHANGELOG.md) | see what changed between releases |

The API reference with runnable examples is on
[pkg.go.dev](https://pkg.go.dev/github.com/vladislavprovich/goarena), or
locally with `go doc -all github.com/vladislavprovich/goarena/store`.

Runnable programs live in [../examples](../examples): `quickstart` (tour of
the store API), `typed-arena` (typed values and the `Slab` allocator),
`gc-pacing` (the effect of `Config.GCPacing`) and `httpcache` (an HTTP cache
service with a live dashboard).
