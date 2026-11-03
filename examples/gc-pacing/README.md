# gc-pacing

Shows what `store.Config{GCPacing: true}` changes for a service that keeps
its data off-heap and allocates Go garbage per request.

The program fills a store with 100 000 values of 512 bytes and runs the same
request loop twice (300 000 requests, about 2 KiB of garbage each), measuring
the GC with `gcstats.Read` / `gcstats.Between`:

- **without pacing** the live Go heap is a few MiB, so the heap goal is a few
  MiB too and the garbage triggers a GC cycle every few MiB of allocation;
- **with pacing** the default `gcpace` pacer raises the GC percent so that
  the heap goal is the one a heap-resident cache of the same size would get,
  and the same work runs with far fewer GC cycles.

```sh
go run ./examples/gc-pacing
```

Typical output (numbers vary by machine):

```
variant           GC cycles   GC CPU %   heap goal MiB
GCPacing: false         179      1.71%               7
GCPacing: true            5      0.05%             132

pacer: off-heap 128 MiB, live heap 3 MiB -> GC percent 3300
GC cycles reduced 36x
```

The pacer changes the process-wide GOGC/GOMEMLIMIT and restores them when the
last paced store is closed. Built without cgo, the store lives in the Go heap,
`GCPacing` has no effect and both variants behave alike (the program says
so). Background: [docs/GUIDE.md](../../docs/GUIDE.md#gc-pacing-and-gomemlimit)
and [docs/DESIGN.md, section 15](../../docs/DESIGN.md#15-gc-pacing-for-off-heap-data-gcpace-v030).
