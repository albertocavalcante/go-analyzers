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
- **`sortmigrate`**: Detects deprecated `sort.Strings`, `sort.Ints`, `sort.Float64s`, `sort.Slice`, `sort.SliceStable`, `sort.SliceIsSorted`, and their `AreSorted` variants, suggesting `slices.Sort`, `slices.SortFunc`, `slices.IsSorted`, etc.

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
