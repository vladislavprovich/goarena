# typed-arena

The lower-level allocators of `github.com/vladislavprovich/goarena/arena`:

- `arena.NewValue` / `arena.NewSlice` place pointer-free structs in a
  bump-pointer `Arena` (types containing Go pointers, such as strings, are
  refused with `arena.ErrPointerType`);
- `Arena.Alloc` / `Arena.Bytes` work with raw `Ref`s (plain integers);
- a `Slab` frees individual blocks and reuses them through per-class free
  lists.

```sh
go run ./examples/typed-arena
```

Values allocated in an arena are valid until `Reset` or `Release`; never keep
pointers to them after that.
