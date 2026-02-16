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

## Bazel nogo Integration

### What is nogo?
nogo is a static analysis tool built into Bazel's [rules_go](https://github.com/bazel-contrib/rules_go).
It runs `go/analysis`-based analyzers as part of the build process itself -- every
`go_library`, `go_binary`, and `go_test` target is analyzed automatically. Because
it runs inside Bazel's action graph, analysis results are cached and parallelized
just like compilation.

### How it integrates with `go/analysis`
nogo uses **static code generation** (via `generate_nogo_main.go`) to produce a
binary that imports every analyzer package listed in the `nogo()` rule's `deps`
attribute. Each package **must** export a variable named `Analyzer` of type
`*analysis.Analyzer`. The generated binary calls `analysis.Validate` on all
analyzers at init-time, then invokes each analyzer once per Go package being
compiled.

This is the same `Analyzer` variable convention we already follow in each of our
packages (`makecopy.Analyzer`, `searchmigrate.Analyzer`, `clampcheck.Analyzer`),
so our analyzers are nogo-compatible out of the box.

### `nogo()` rule attributes

| Attribute | Type | Description |
|-----------|------|-------------|
| `name` | string | Unique target name (mandatory) |
| `deps` | label_list | `go_library` targets exporting an `Analyzer` variable |
| `config` | label | JSON file controlling per-analyzer behavior |
| `vet` | bool | If `True`, adds a safe subset of vet analyzers (atomic, bools, buildtag, nilfunc, printf) |

### Config JSON format
The top-level keys must match `Analyzer.Name` for each registered analyzer.
A special `_base` key provides defaults inherited by all analyzers.

```json
{
  "_base": {
    "exclude_files": {
      "external/": "skip all external dependencies",
      "third_party/": "skip vendored code"
    }
  },
  "makecopy": {
    "only_files": {
      "src/.*": "only analyze first-party source"
    }
  },
  "searchmigrate": {
    "exclude_files": {
      "generated\\.go$": "skip generated files"
    }
  },
  "clampcheck": {},
  "printf": {
    "analyzer_flags": {
      "funcs": "Wrapf,Errorf"
    }
  }
}
```

Supported fields per analyzer:
- `only_files` — regex-keyed map; analyzer only reports on matching files.
- `exclude_files` — regex-keyed map; overrides `only_files` for matching files.
  Values are description strings explaining the exclusion.
- `analyzer_flags` — map of flag names (without `-` prefix) to string values,
  passed to the analyzer via its `analysis.Analyzer.Flags` field.

### Registering nogo

**WORKSPACE (legacy)**:
```python
load("@io_bazel_rules_go//go:deps.bzl", "go_rules_dependencies", "go_register_nogo")
go_rules_dependencies()
go_register_toolchains(version = "1.23.1")
go_register_nogo(nogo = "@//:my_nogo")
```

**Bzlmod (MODULE.bazel)**:
```python
go_sdk = use_extension("@rules_go//go:extensions.bzl", "go_sdk")
go_sdk.nogo(nogo = "//:my_nogo")
```

With Bzlmod you can scope analysis to specific packages:
```python
go_sdk.nogo(
    nogo = "//:my_nogo",
    includes = ["//:__subpackages__"],
    excludes = ["//third_party:__subpackages__"],
)
```

### nogo vs golangci-lint

| Aspect | nogo | golangci-lint |
|--------|------|---------------|
| When it runs | Build-time (inside `bazel build`) | Separate CLI invocation |
| Caching | Bazel's incremental build cache | Its own cache |
| Failure mode | Fails the build on any diagnostic | Exit code / CI gate |
| Auto-fix | Generates patch files, cannot apply inline | `--fix` flag applies directly |
| Line-level suppression | Not supported (file-level `exclude_files` only) | `//nolint` comments |
| Analyzer discovery | One exported `Analyzer` per Go package | Plugin system or built-in registry |
| Config granularity | Global JSON, regex-based file matching | Rich YAML, per-linter settings |
| Best for | Large Bazel monorepos, enforcing build-time gates | General Go projects, developer workflow |

### Gotchas and limitations
- **Diagnostics fail the build.** Only emit diagnostics for issues severe enough to
  block a build. In golangci-lint, warnings are common; in nogo, every diagnostic
  is a hard failure.
- **One `Analyzer` per Go package.** nogo discovers analyzers by looking for a
  package-level `var Analyzer`. Tools like staticcheck that bundle many analyzers in
  one package need wrapper packages (one per analyzer) for nogo compatibility.
- **No `//nolint`-style suppression.** Suppression is file-level only via
  `exclude_files` in the config JSON. There is no way to suppress a single finding
  on one line.
- **External repos.** WORKSPACE mode analyzes external repos by default; Bzlmod
  excludes them by default. Either way, many external packages will not pass strict
  analysis, so `exclude_files` with `"external/"` is almost always needed.
- **Config changes trigger full rebuild.** Changing `nogo_config.json` invalidates
  cached analysis results for all Go targets.
- **Fix application is indirect.** Analyzers can produce `SuggestedFix` values, but
  nogo writes them as patch files. You must extract and apply them manually:
  ```bash
  bazel build //... --norun_validations --output_groups nogo_fix
  ```
- **Compilation must succeed first.** nogo runs after the Go compiler; if a file
  does not compile, it is not analyzed.
- **Tag `"no-nogo"` to skip.** Add the tag to any target where analysis should be
  skipped (useful for large generated code).

### Useful links
- [Official nogo docs (nogo.rst)](https://github.com/bazel-contrib/rules_go/blob/master/go/nogo.rst)
- [rules_go Bzlmod docs](https://github.com/bazel-contrib/rules_go/blob/master/docs/go/core/bzlmod.md)
- [sluongng/nogo-analyzer](https://github.com/sluongng/nogo-analyzer) — Example of
  wrapping staticcheck/golangci-lint analyzers for nogo
- [nogo_main.go](https://github.com/bazel-contrib/rules_go/blob/master/go/tools/builders/nogo_main.go) —
  The generated nogo runner source

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
