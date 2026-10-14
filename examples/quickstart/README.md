# quickstart

A five-minute tour of the off-heap key-value store
(`github.com/vladislavprovich/goarena/store`).

What it shows, in order:

| step | API |
|---|---|
| create and close a store | `store.New(store.Config{})`, `defer s.Close()` |
| write | `Set`, `SetString` |
| read a copy, into a reused buffer, zero-copy | `Get`, `GetInto`, `View` |
| cache-aside: load on a miss, cache the result | `GetOrLoad` |
| set only if absent | `GetOrSet` |
| time to live and eviction | `SetStringWithTTL`, `HasString`, `EvictExpired` |
| delete | `Delete` |
| error details and `errors.Is` | `ErrTooLarge` |
| statistics | `Stats`, `Stats.String` |
| iterate | `Range` |
| behaviour after `Close` | `Closed`, `ErrClosed` |

Run it from the repository root:

```sh
go run ./examples/quickstart
```

The output prints one line per step, for example
`GetOrLoad  user:3 -> {"name":"Linus"} (loader called 1 time)`.

It needs cgo for off-heap memory (`CGO_ENABLED=1` and a C compiler). Without
cgo it still runs and prints a note that the data is kept in the Go heap; see
[docs/GUIDE.md](../../docs/GUIDE.md#builds-without-cgo). Always `Close` a
store: its memory is not reclaimed by the garbage collector.
