# typed-arena

The lower-level allocators of `github.com/vladislavprovich/goarena/arena`,
for pointer-free typed data rather than key-value pairs.

What it shows:

- `arena.IsPointerFree`, `arena.NewValue` and `arena.NewSlice` place structs
  made only of numbers in a bump-pointer `Arena`; a type containing Go
  pointers (here a `string`) is refused with `arena.ErrPointerType`;
- `Arena.Alloc` / `Arena.Bytes` work with raw `Ref`s (plain 64-bit integers
  the garbage collector never looks at);
- a `Slab` frees individual blocks and reuses them through per-class free
  lists; its statistics show the size-class rounding.

```sh
go run ./examples/typed-arena
```

Values allocated in an arena are valid until `Reset` or `Release`; never keep
pointers to them after that. A `Slab` is not safe for concurrent use. More in
[docs/GUIDE.md](../../docs/GUIDE.md#typed-data-with-the-arena-package).
