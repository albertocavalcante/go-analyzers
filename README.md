# go-analyzers

Custom Go static analyzers for modern Go 1.25+ idioms. These analyzers detect
patterns that can be simplified using newer standard library functions.

Usable standalone via `go vet`, or as a [golangci-lint v2 module plugin](https://golangci-lint.run/docs/plugins/module-plugins/).

## Analyzers

| Analyzer | Detects | Suggests |
|---|---|---|
| `makecopy` | `make([]T, len(s)); copy(dst, s)` | `slices.Clone(s)` |
| `searchmigrate` | `sort.Search(n, func(i int) bool { ... })` | `slices.BinarySearch(s, v)` |
| `clampcheck` | `if x < lo { x = lo } else if x > hi { x = hi }` | `min(max(x, lo), hi)` |

## Why these analyzers?

The official [modernize](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/modernize)
suite and [revive](https://github.com/mgechev/revive)'s `use-slices-sort` rule cover most
Go 1.21+ modernization patterns. These analyzers fill the remaining gaps:

- **`makecopy`**: `modernize`'s `appendclipped` only catches `append`-based clones, not `make`+`copy`.
- **`searchmigrate`**: No existing linter detects `sort.Search` → `slices.BinarySearch`.
- **`clampcheck`**: `modernize`'s `minmax` handles simple `if/else` → `min`/`max` but deliberately excludes nested `if-elseif-else` clamp patterns.

## Installation

### Standalone

```bash
go install github.com/albertocavalcante/go-analyzers/cmd/go-analyzers@latest
go vet -vettool=$(which go-analyzers) ./...
```

### golangci-lint v2 module plugin

1. Create `.custom-gcl.yml` in your project:

```yaml
version: v2.9.0
plugins:
  - module: 'github.com/albertocavalcante/go-analyzers'
    version: v0.1.0
```

2. Build custom binary:

```bash
golangci-lint custom
```

3. Add to your golangci-lint config:

```toml
[linters.settings.custom.go-analyzers]
type = "module"
description = "Custom analyzers for modern Go idioms"
```

4. Run:

```bash
./custom-gcl run ./...
```

## License

MIT
