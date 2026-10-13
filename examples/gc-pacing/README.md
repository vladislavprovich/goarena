# gc-pacing

Shows what `store.Config{GCPacing: true}` changes for a service that keeps
its data off-heap and allocates Go garbage per request.

The program fills a store with ~50 MiB of values and runs the same request
loop twice, measuring the GC with `gcstats.Read`/`gcstats.Between`:

- **without pacing** the live Go heap is a few MiB, so the heap goal is a few
  MiB too and the garbage triggers a GC cycle every few MiB of allocation;
- **with pacing** the default `gcpace` pacer raises the GC percent so that
  the heap goal is the one a heap-resident cache of the same size would get,
  and the same work runs with far fewer GC cycles.

```sh
go run ./examples/gc-pacing
```

The pacer changes the process-wide GOGC/GOMEMLIMIT and restores them when the
last paced store is closed. See `docs/DESIGN.md`, section 15.
