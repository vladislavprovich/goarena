# httpcache

An HTTP key-value service built on `github.com/vladislavprovich/goarena/store`
with a self-contained live dashboard (`web/dashboard.html`, embedded into the
binary).

| Route | Description |
|---|---|
| `PUT /kv/{key}` | store the request body under key |
| `GET /kv/{key}` | return the value (404 if absent) |
| `DELETE /kv/{key}` | delete the key |
| `GET /stats` | JSON metrics: ops/s, latency, GC, memory, store statistics |
| `GET /stats/history` | the last 120 one-second snapshots |
| `GET /dashboard` | HTML dashboard with live charts (`/` redirects here) |
| `GET /healthz` | liveness probe |

```sh
go run ./examples/httpcache -selfload -keys 2000000          # goarena backend
go run ./examples/httpcache -selfload -backend gomap         # Go map, for comparison
```

Then open <http://127.0.0.1:8080/dashboard>. `-selfload` generates traffic
in-process; run with `-h` for all flags.

| File | Contents |
|---|---|
| `main.go` | flags, backend selection, server lifecycle |
| `cache_backends.go` | the `Backend` interface, goarena and Go-map backends |
| `http_server.go` | routes and HTTP handlers |
| `live_statistics.go` | latency windows, counters and the once-per-second sampler |
| `self_load_generator.go` | in-process load generator |
| `web/dashboard.html` | the dashboard page |
