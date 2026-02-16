# Learnings

Notes and discoveries from building this analyzer suite.

## Go Static Analysis Framework

### Key packages
- `golang.org/x/tools/go/analysis` — The core framework. An `*analysis.Analyzer`
  describes a single analysis pass.
- `golang.org/x/tools/go/analysis/passes/inspect` — Provides an `*inspector.Inspector`
  for efficient AST traversal. Always use this as a dependency rather than walking
  the AST manually.
- `golang.org/x/tools/go/analysis/analysistest` — Test harness. Uses `// want "regexp"`
  comments in testdata source files to assert diagnostics.
- `golang.org/x/tools/go/analysis/multichecker` — Combines multiple analyzers into a
  single binary usable with `go vet -vettool=`.

### Writing an analyzer
1. Define a `var Analyzer = &analysis.Analyzer{...}` with Name, Doc, Requires, and Run.
2. In `Run(pass *analysis.Pass)`, use `pass.ResultOf[inspect.Analyzer]` to get the
   inspector, then call `Preorder` or `Nodes` with a node filter.
3. Use `pass.TypesInfo` to resolve identifiers to their declarations — this is essential
   for distinguishing builtins from user-defined functions with the same name.
4. Call `pass.Reportf(pos, msg, args...)` to emit diagnostics.

### Testing
- Create `testdata/src/<pkgname>/<pkgname>.go` files with `// want "..."` comments.
- The test harness compiles the testdata package and runs the analyzer, checking that
  each `// want` comment matches a diagnostic on that line.
- The `// want` value is a regexp, so escape special characters (e.g., `\\+` for `+`).

### Gotchas
- `pass.TypesInfo.ObjectOf(ident)` returns nil for builtin functions like `make`,
  `copy`, `len`. Check for `obj == nil || obj.Pkg() == nil` to identify builtins.
- `ast.CallExpr.Fun` can be an `*ast.Ident` (for builtins or same-package calls)
  or an `*ast.SelectorExpr` (for qualified calls like `sort.Search`).
- When matching consecutive statements, iterate over `BlockStmt.List` pairwise.

## golangci-lint v2 Module Plugin System

### How it works
1. Your module exports a plugin via `init()` calling `register.Plugin(name, constructor)`.
2. The constructor returns a `register.LinterPlugin` with `BuildAnalyzers()` and `GetLoadMode()`.
3. Users create `.custom-gcl.yml` specifying your module, run `golangci-lint custom` to
   build a custom binary, then reference the plugin in their golangci config.

### Load modes
- `register.LoadModeSyntax` — Only AST, no type info. Fastest but limited.
- `register.LoadModeTypesInfo` — AST + type information. Required for most non-trivial analyzers.

### Key dependency
```
github.com/golangci/plugin-module-register v0.1.2
```

## Existing coverage by other tools

Before writing a custom analyzer, check these tools:

| Tool | What it covers |
|---|---|
| `modernize` (gopls/go fix) | `any`, `min`/`max`, `slices.Sort`, `slices.Contains`, `maps.Clone`, `strings.Cut`, `CutPrefix`, `fmt.Appendf`, `range int`, iterators, `wg.Go()` |
| `revive` `use-slices-sort` | `sort.Strings`→`slices.Sort`, `sort.Slice`→`slices.SortFunc`, all 12 sort→slices patterns |
| `exptostd` | `golang.org/x/exp/slices`→`slices`, `x/exp/maps`→`maps` |
| `intrange` | `for i := 0; i < n; i++`→`for i := range n` |
| `copyloopvar` | Redundant `v := v` in Go 1.22+ |

### What's NOT covered (why these analyzers exist)
- `make` + `copy` → `slices.Clone` (modernize only catches `append`-based clones)
- `sort.Search` → `slices.BinarySearch` (no existing linter)
- Compound clamp `if-elseif-else` → `min(max(...))` (modernize deliberately excludes this)

## Useful commands

```bash
# Run all analyzers on a project
go vet -vettool=$(go build -o /dev/null ./cmd/go-analyzers && echo ./cmd/go-analyzers) ./...

# Or install and run
go install ./cmd/go-analyzers
go vet -vettool=$(which go-analyzers) ./...

# Run tests
go test ./...

# Run a specific analyzer's tests
go test ./makecopy/...
go test ./searchmigrate/...
go test ./clampcheck/...

# Update test expectations (if using analysistest with -update flag)
go test ./... -update

# Build golangci-lint custom binary with this plugin
# In the consuming project:
golangci-lint custom  # reads .custom-gcl.yml
./custom-gcl run ./...

# Check what golangci-lint version supports
golangci-lint version
golangci-lint linters
```
