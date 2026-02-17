# go-analyzers

Custom Go static analyzers for modern Go 1.25+ idioms. These analyzers detect
patterns that can be simplified using newer standard library functions.

Usable standalone via `go vet` or with Bazel's [nogo](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst) for build-time analysis.

## Analyzers

| Analyzer | Detects | Suggests |
|---|---|---|
| `makecopy` | `make([]T, len(s)); copy(dst, s)` (including subslice variants) | `slices.Clone(s)` |
| `searchmigrate` | `sort.Search(n, func(i int) bool { ... })` | `slices.BinarySearch(s, v)` |
| `clampcheck` | if-else-if clamp chains and consecutive if-return clamp patterns | `min(max(x, lo), hi)` |
| `sortmigrate` | `sort.Strings`, `sort.Ints`, `sort.Slice`, etc. | `slices.Sort`, `slices.SortFunc`, etc. |

## Why these analyzers?

The official [modernize](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/modernize)
suite and [revive](https://github.com/mgechev/revive)'s `use-slices-sort` rule cover most
Go 1.21+ modernization patterns. These analyzers fill the remaining gaps:

- **`makecopy`**: `modernize`'s `appendclipped` only catches `append`-based clones, not `make`+`copy`. Also detects subslice variants like `make([]T, len(s)-idx); copy(dst, s[idx:])`.
- **`searchmigrate`**: No existing linter detects `sort.Search` → `slices.BinarySearch`.
- **`clampcheck`**: `modernize`'s `minmax` handles simple `if/else` → `min`/`max` but deliberately excludes nested `if-elseif-else` clamp patterns. Also detects consecutive if-return clamp patterns.
- **`sortmigrate`**: Detects deprecated `sort.Strings`, `sort.Ints`, `sort.Float64s`, `sort.Slice`, `sort.SliceStable`, `sort.SliceIsSorted`, and their `AreSorted` variants, suggesting `slices.Sort`, `slices.SortFunc`, `slices.IsSorted`, etc. Includes auto-fix for `sort.Slice` callback rewriting — a gap the Go team's `modernize` [explicitly deferred](https://github.com/golang/go/issues/67795).

## sortmigrate: auto-fix deep dive

`sortmigrate` handles two categories of sort-to-slices migrations:

**Direct replacements** (always auto-fixed) — the function signature is identical:

```
sort.Strings(s)           →  slices.Sort(s)
sort.Ints(s)              →  slices.Sort(s)
sort.Float64s(s)          →  slices.Sort(s)
sort.IntsAreSorted(s)     →  slices.IsSorted(s)
sort.StringsAreSorted(s)  →  slices.IsSorted(s)
sort.Float64sAreSorted(s) →  slices.IsSorted(s)
```

**Callback rewrites** (auto-fixed when the pattern is recognized) — the callback
signature changes from `func(i, j int) bool` to `func(a, b T) int`:

```
sort.Slice(s, less)          →  slices.SortFunc(s, cmp)
sort.SliceStable(s, less)    →  slices.SortStableFunc(s, cmp)
sort.SliceIsSorted(s, less)  →  slices.IsSortedFunc(s, cmp)
```

### What the fixer rewrites automatically

The fixer handles single-return comparison callbacks. It infers the element type
from the slice argument, rewrites index-based access (`s[i]`, `s[j]`) to
value-based parameters (`a`, `b`), and converts boolean comparison to
`cmp.Compare`:

```go
// Before
sort.Slice(items, func(i, j int) bool {
    return items[i].Name < items[j].Name
})

// After (auto-fixed)
slices.SortFunc(items, func(a, b Item) int {
    return cmp.Compare(a.Name, b.Name)
})
```

Supported patterns:

| Pattern | Example | Result |
|---|---|---|
| Direct comparison | `s[i] < s[j]` | `cmp.Compare(a, b)` |
| Field access | `s[i].Name < s[j].Name` | `cmp.Compare(a.Name, b.Name)` |
| Method call | `s[i].Key() < s[j].Key()` | `cmp.Compare(a.Key(), b.Key())` |
| Chained access | `s[i].Inner.Key < s[j].Inner.Key` | `cmp.Compare(a.Inner.Key, b.Inner.Key)` |
| Reversed (`>`) | `s[i] > s[j]` | `cmp.Compare(b, a)` |
| Swapped params | `s[j] < s[i]` | `cmp.Compare(b, a)` |
| Pointer elements | `[]*Item` with `s[i].F < s[j].F` | `func(a, b *Item) int { ... }` |
| Cross-package types | `[]fs.DirEntry` (when `"io/fs"` is imported) | `func(a, b fs.DirEntry) int { ... }` |
| All operators | `<`, `>`, `<=`, `>=` | Correctly mapped |
| All three functions | `Slice`, `SliceStable`, `SliceIsSorted` | `SortFunc`, `SortStableFunc`, `IsSortedFunc` |

### What stays report-only (and why)

These cases emit a diagnostic but no auto-fix. The developer must migrate manually.

**Multi-statement callbacks:**

```go
sort.Slice(items, func(i, j int) bool {
    if items[i].Priority != items[j].Priority {
        return items[i].Priority < items[j].Priority
    }
    return items[i].Name < items[j].Name
})
```

This is a multi-key sort. The correct migration is a cascading `cmp.Compare`
chain, but recognizing arbitrary multi-key patterns reliably is complex. A future
version may handle the common cascading-if pattern.

**Non-inline callbacks:**

```go
less := func(i, j int) bool { return s[i] < s[j] }
sort.Slice(s, less)  // variable reference, not a func literal
```

The fixer only analyzes inline `func` literals. Tracing variable definitions
across scopes is fragile and out of scope.

**Cross-package types without import:**

```go
// File does NOT import "io/fs"
func process(entries []fs.DirEntry) {
    sort.Slice(entries, func(i, j int) bool { ... })
}
```

The fix would generate `func(a, b fs.DirEntry)` but `"io/fs"` isn't imported.
Adding arbitrary package imports is outside the fixer's scope. If the file already
imports the package, the fix proceeds normally.

**Aliased cross-package imports:**

```go
import myfs "io/fs"
```

The generated code uses the canonical package name (`fs.DirEntry`) from the type
system, which won't match an alias (`myfs`). The fixer bails out rather than
produce code that references an undefined name.

**Non-identifier slice arguments:**

```go
sort.Slice(obj.GetItems(), func(i, j int) bool { ... })
```

The fixer matches the slice variable name in the callback body (e.g., `s[i]`
must match the `s` passed to `sort.Slice`). When the slice argument is a method
call or field access rather than a simple variable name, name matching can't work.

### The fundamental limitation

`sort.Slice` uses a **less** function (`func(i, j int) bool`) while
`slices.SortFunc` uses a **comparison** function (`func(a, b T) int`). These have
different information content:

- `less(a, b) = true` means `a < b` (→ return `-1`)
- `less(a, b) = false` means `a >= b`, but we **don't know** if `a == b` or `a > b`

A correct general conversion requires **two** calls to the original less function:

```go
slices.SortFunc(s, func(a, b T) int {
    if less(a, b) { return -1 }  // a < b
    if less(b, a) { return 1 }   // a > b
    return 0                      // a == b
})
```

This is semantically correct but doubles the comparison cost and produces ugly
code that a developer should simplify by hand. That's why the fixer only handles
patterns where it can emit a clean, single-evaluation `cmp.Compare` call —
anything else is better left to the developer.

### vs modernize's slicessort

Go's official `modernize` analyzer includes a `slicessort` check, but it only
handles the **trivial** case where the entire closure can be dropped:

```go
sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
→ slices.Sort(s)  // modernize handles this
```

The `sort.Slice → slices.SortFunc` conversion (where the callback must be
rewritten, not dropped) was [explicitly deferred](https://github.com/golang/go/issues/67795)
by the Go team as too complex. That's the gap this analyzer fills.

## Installation

### Standalone

```bash
go install github.com/albertocavalcante/go-analyzers/cmd/go-analyzers@latest
go vet -vettool=$(which go-analyzers) ./...
```

### golangci-lint v2 module plugin

For golangci-lint integration, see [go-analyzers-gcl](https://github.com/albertocavalcante/go-analyzers-gcl).

### Bazel nogo (build-time analysis)

[nogo](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst) runs
`go/analysis` analyzers as part of `bazel build`, failing the build on any
diagnostic. Each of our analyzer packages exports a `var Analyzer` compatible
with nogo's requirements.

#### Bzlmod (MODULE.bazel) -- recommended

1. Add this module as a dependency in your `MODULE.bazel`:

```starlark
bazel_dep(name = "go-analyzers", version = "0.1.0")

go_deps = use_extension("@gazelle//:extensions.bzl", "go_deps")
go_deps.from_file(go_mod = "//:go.mod")
# If not using go.mod, add the module directly:
# go_deps.module(
#     path = "github.com/albertocavalcante/go-analyzers",
#     version = "v0.1.0",
# )
```

2. Define a `nogo()` target in your root `BUILD.bazel`:

```starlark
load("@rules_go//go:def.bzl", "nogo", "TOOLS_NOGO")

nogo(
    name = "my_nogo",
    deps = TOOLS_NOGO + [
        "@com_github_albertocavalcante_go_analyzers//makecopy",
        "@com_github_albertocavalcante_go_analyzers//searchmigrate",
        "@com_github_albertocavalcante_go_analyzers//clampcheck",
        "@com_github_albertocavalcante_go_analyzers//sortmigrate",
    ],
    config = ":nogo_config.json",
    vet = True,
    visibility = ["//visibility:public"],
)
```

3. Register nogo with the Go SDK in `MODULE.bazel`:

```starlark
go_sdk = use_extension("@rules_go//go:extensions.bzl", "go_sdk")
go_sdk.nogo(nogo = "//:my_nogo")
```

#### WORKSPACE (legacy)

1. Add this module via `go_repository` (typically managed by Gazelle):

```starlark
load("@bazel_gazelle//:deps.bzl", "go_repository")

go_repository(
    name = "com_github_albertocavalcante_go_analyzers",
    importpath = "github.com/albertocavalcante/go-analyzers",
    sum = "h1:...",
    version = "v0.1.0",
)
```

2. Define the `nogo()` target in your root `BUILD` file (same as Bzlmod above).

3. Register nogo in your `WORKSPACE`:

```starlark
load("@io_bazel_rules_go//go:deps.bzl", "go_register_nogo")
go_register_nogo(nogo = "@//:my_nogo")
```

#### nogo config JSON

Create `nogo_config.json` in your repository root to control analyzer behavior:

```json
{
  "_base": {
    "exclude_files": {
      "external/": "skip external dependencies",
      "third_party/": "skip vendored code"
    }
  },
  "makecopy": {},
  "searchmigrate": {},
  "clampcheck": {},
  "sortmigrate": {}
}
```

See the [nogo documentation](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst)
for the full config format (`only_files`, `exclude_files`, `analyzer_flags`).

#### Notes

- nogo runs automatically on every `bazel build` -- no separate lint step needed.
- Diagnostics are **build failures**. Use `exclude_files` to suppress false positives
  rather than `//nolint` comments (which nogo does not support).
- Add `tags = ["no-nogo"]` to any target where analysis should be skipped.
- Use `--norun_validations` to temporarily skip nogo during a build.

## License

MIT
