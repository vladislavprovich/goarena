# quickstart

A five-minute tour of the off-heap key-value store
(`github.com/vladislavprovich/goarena/store`): create a store, write and read
entries with `Set`/`Get`/`GetInto`/`View`, use a TTL, delete, iterate with
`Range`, print `Stats` and `Close` the store.

```sh
go run ./examples/quickstart
```

Requires cgo (`CGO_ENABLED=1` and a C compiler), like the rest of the module.
Always `Close` a store: its memory lives outside the Go heap and is not
reclaimed by the garbage collector.
