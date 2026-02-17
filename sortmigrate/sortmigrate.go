// Package sortmigrate defines an analyzer that detects deprecated sort package
// function calls that can be replaced with slices package equivalents.
//
// # Analyzer sortmigrate
//
// sortmigrate: detect sort.Xyz calls that can use slices equivalents
//
// This analyzer flags calls to deprecated sort package functions:
//
//   - sort.Strings(s)              -> slices.Sort(s)
//   - sort.Ints(s)                 -> slices.Sort(s)
//   - sort.Float64s(s)             -> slices.Sort(s)
//   - sort.Slice(s, less)          -> slices.SortFunc(s, cmp)
//   - sort.SliceStable(s, less)    -> slices.SortStableFunc(s, cmp)
//   - sort.SliceIsSorted(s, less)  -> slices.IsSortedFunc(s, cmp)
//   - sort.IntsAreSorted(s)        -> slices.IsSorted(s)
//   - sort.StringsAreSorted(s)     -> slices.IsSorted(s)
//   - sort.Float64sAreSorted(s)    -> slices.IsSorted(s)
//
// For sort.Slice, sort.SliceStable, and sort.SliceIsSorted, auto-fix is provided
// when the callback is a simple single-return comparison (e.g. s[i] < s[j] or
// s[i].Field < s[j].Field). Complex callbacks remain report-only.
//
// Available since Go 1.21.
package sortmigrate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/albertocavalcante/go-analyzers/internal/importutil"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "sortmigrate",
	Doc:      "detect sort.Xyz calls that can use slices equivalents",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// migrations maps sort package function names to their slices package replacements.
var migrations = map[string]string{
	"Strings":           "slices.Sort",
	"Ints":              "slices.Sort",
	"Float64s":          "slices.Sort",
	"Slice":             "slices.SortFunc",
	"SliceStable":       "slices.SortStableFunc",
	"SliceIsSorted":     "slices.IsSortedFunc",
	"IntsAreSorted":     "slices.IsSorted",
	"StringsAreSorted":  "slices.IsSorted",
	"Float64sAreSorted": "slices.IsSorted",
}

// callbackMigrations lists sort functions that take a callback whose signature
// differs between sort and slices (func(i, j int) bool vs func(a, b T) int).
// These require callback rewriting and can only be auto-fixed for simple patterns.
var callbackMigrations = map[string]bool{
	"Slice":         true,
	"SliceStable":   true,
	"SliceIsSorted": true,
}

// pendingDiag holds a diagnostic and its associated edits before import edits
// are attached. This allows collecting all needed imports per file first,
// then creating a single combined import TextEdit to avoid conflicts.
type pendingDiag struct {
	diag    analysis.Diagnostic
	edits   []analysis.TextEdit
	imports []string // packages needed (e.g., "slices", "cmp")
	file    string   // file name from Fset
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	var pending []pendingDiag

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}

		funcName := sel.Sel.Name
		replacement, ok := migrations[funcName]
		if !ok {
			return
		}

		// Verify the receiver is the "sort" package.
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return
		}

		obj := pass.TypesInfo.ObjectOf(ident)
		if obj == nil {
			return
		}

		pkgName, ok := obj.(*types.PkgName)
		if !ok {
			return
		}

		if pkgName.Imported().Path() != "sort" {
			return
		}

		msg := fmt.Sprintf("sort.%s can be replaced with %s", funcName, replacement)
		diag := analysis.Diagnostic{Pos: call.Pos(), Message: msg}
		fileName := pass.Fset.File(call.Pos()).Name()

		if callbackMigrations[funcName] {
			// Try to build auto-fix for the callback.
			edits := tryBuildSliceFix(pass, call, sel, replacement)
			if edits != nil {
				pending = append(pending, pendingDiag{
					diag:    diag,
					edits:   edits,
					imports: []string{"cmp", "slices"},
					file:    fileName,
				})
			} else {
				// Complex callback — report-only, no auto-fix.
				pass.Report(diag)
			}
		} else {
			edits := []analysis.TextEdit{
				{Pos: sel.Pos(), End: sel.Sel.End(), NewText: []byte(replacement)},
			}
			pending = append(pending, pendingDiag{
				diag:    diag,
				edits:   edits,
				imports: []string{"slices"},
				file:    fileName,
			})
		}
	})

	// Collect all needed imports per file.
	fileImports := map[string]map[string]bool{}
	filePosMap := map[string]token.Pos{}
	for _, pd := range pending {
		if fileImports[pd.file] == nil {
			fileImports[pd.file] = map[string]bool{}
			filePosMap[pd.file] = pd.diag.Pos
		}
		for _, pkg := range pd.imports {
			fileImports[pd.file][pkg] = true
		}
	}

	// Build a single combined import TextEdit per file.
	fileImportEdits := map[string]*analysis.TextEdit{}
	for fileName, pkgSet := range fileImports {
		file := importutil.FindFileForPos(pass, filePosMap[fileName])
		if file == nil {
			continue
		}
		// Build package list in alphabetical order ("cmp" < "slices").
		var pkgs []string
		if pkgSet["cmp"] {
			pkgs = append(pkgs, "cmp")
		}
		if pkgSet["slices"] {
			pkgs = append(pkgs, "slices")
		}
		if edit := importutil.AddMultipleImportsEdit(file, pkgs); edit != nil {
			fileImportEdits[fileName] = edit
		}
	}

	// Attach import edits to the first diagnostic per file and report all.
	importAttached := map[string]bool{}
	for _, pd := range pending {
		// Clone edits to avoid mutating pd.edits when appending import edits.
		allEdits := append([]analysis.TextEdit{}, pd.edits...)
		if !importAttached[pd.file] {
			if ie, ok := fileImportEdits[pd.file]; ok {
				allEdits = append(allEdits, *ie)
				importAttached[pd.file] = true
			}
		}
		pd.diag.SuggestedFixes = []analysis.SuggestedFix{
			{Message: pd.diag.Message, TextEdits: allEdits},
		}
		pass.Report(pd.diag)
	}

	return nil, nil
}

// tryBuildSliceFix attempts to build TextEdits for sort.Slice/SliceStable/SliceIsSorted
// calls when the callback is a simple single-return comparison. Returns nil if the
// callback is too complex for auto-fix.
//
// Supported patterns (single return with binary </>/<=/>=):
//   - sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
//   - sort.Slice(s, func(i, j int) bool { return s[i].Field < s[j].Field })
//   - sort.Slice(s, func(i, j int) bool { return s[i].Method() < s[j].Method() })
//   - sort.Slice(s, func(i, j int) bool { return s[i] > s[j] })  (reversed)
//   - sort.Slice(s, func(i, j int) bool { return s[j] < s[i] })  (swapped params)
func tryBuildSliceFix(pass *analysis.Pass, call *ast.CallExpr, sel *ast.SelectorExpr, replacement string) []analysis.TextEdit {
	if len(call.Args) != 2 {
		return nil
	}

	sliceArg := call.Args[0]
	funcLit, ok := call.Args[1].(*ast.FuncLit)
	if !ok {
		return nil
	}

	// Must be func(i, j int) bool — extract param names.
	params := funcLit.Type.Params
	if params == nil {
		return nil
	}
	var iParam, jParam string
	switch {
	case len(params.List) == 1 && len(params.List[0].Names) == 2:
		iParam = params.List[0].Names[0].Name
		jParam = params.List[0].Names[1].Name
	case len(params.List) == 2 && len(params.List[0].Names) == 1 && len(params.List[1].Names) == 1:
		iParam = params.List[0].Names[0].Name
		jParam = params.List[1].Names[0].Name
	default:
		return nil
	}

	// Body must be a single return statement.
	if funcLit.Body == nil || len(funcLit.Body.List) != 1 {
		return nil
	}
	retStmt, ok := funcLit.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(retStmt.Results) != 1 {
		return nil
	}

	// Return expression must be a binary comparison.
	binExpr, ok := retStmt.Results[0].(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	var opReversed bool
	switch binExpr.Op {
	case token.LSS, token.LEQ:
		opReversed = false
	case token.GTR, token.GEQ:
		opReversed = true
	default:
		return nil
	}

	// Slice arg must be a simple identifier.
	sliceIdent, ok := sliceArg.(*ast.Ident)
	if !ok {
		return nil
	}

	// Extract chains from both sides of the comparison.
	lhsChain, lhsParam, lhsOk := extractChain(binExpr.X, sliceIdent.Name)
	rhsChain, rhsParam, rhsOk := extractChain(binExpr.Y, sliceIdent.Name)
	if !lhsOk || !rhsOk {
		return nil
	}

	// Determine param ordering: normal (i on LHS, j on RHS) or swapped.
	// Swapped params reverse the sort direction, same as using > instead of <.
	//   s[i] < s[j]  → ascending      s[j] < s[i]  → descending
	//   s[i] > s[j]  → descending     s[j] > s[i]  → ascending
	var paramsSwapped bool
	if lhsParam == iParam && rhsParam == jParam {
		paramsSwapped = false
	} else if lhsParam == jParam && rhsParam == iParam {
		paramsSwapped = true
	} else {
		return nil
	}

	// Chains must be identical (comparing the same field/method on both elements).
	if lhsChain != rhsChain {
		return nil
	}

	// Descending when exactly one of operator or params is reversed (XOR).
	descending := opReversed != paramsSwapped

	// Infer the element type from the slice argument.
	sliceType := pass.TypesInfo.TypeOf(sliceArg)
	if sliceType == nil {
		return nil
	}
	sliceT, ok := sliceType.Underlying().(*types.Slice)
	if !ok {
		return nil
	}
	elemType := sliceT.Elem()
	// Use a qualifier that returns the package name (not path) for valid Go source.
	// types.RelativeTo returns the full path (e.g., "io/fs"), but source code uses
	// the package name (e.g., "fs").
	qualifier := func(pkg *types.Package) string {
		if pkg == pass.Pkg {
			return ""
		}
		return pkg.Name()
	}
	elemTypeStr := types.TypeString(elemType, qualifier)

	// If the element type references another package (e.g., "fs.DirEntry"),
	// verify that package is already imported without an alias. We can't add
	// arbitrary package imports, but we can proceed if it's already available.
	if strings.Contains(elemTypeStr, ".") {
		if !externalTypeImported(pass, call.Pos(), elemType) {
			return nil
		}
	}

	// Build cmp.Compare arguments.
	chain := lhsChain
	aExpr := "a" + chain
	bExpr := "b" + chain
	if descending {
		aExpr, bExpr = bExpr, aExpr
	}

	newFunc := fmt.Sprintf("func(a, b %s) int { return cmp.Compare(%s, %s) }", elemTypeStr, aExpr, bExpr)

	return []analysis.TextEdit{
		{
			Pos:     sel.Pos(),
			End:     sel.Sel.End(),
			NewText: []byte(replacement),
		},
		{
			Pos:     funcLit.Pos(),
			End:     funcLit.End(),
			NewText: []byte(newFunc),
		},
	}
}

// externalTypeImported checks whether the package of an external named type is
// already imported (without alias) in the file containing pos. This allows
// auto-fixing sort.Slice calls where the element type is from another package
// that the file already uses (e.g., []fs.DirEntry when "io/fs" is imported).
func externalTypeImported(pass *analysis.Pass, pos token.Pos, elemType types.Type) bool {
	// Unwrap pointer if present (e.g., *pkg.Type → pkg.Type).
	if ptr, ok := elemType.(*types.Pointer); ok {
		elemType = ptr.Elem()
	}

	named, ok := elemType.(*types.Named)
	if !ok {
		return false
	}

	typePkg := named.Obj().Pkg()
	if typePkg == nil || typePkg == pass.Pkg {
		return true // built-in or same package — always available
	}

	file := importutil.FindFileForPos(pass, pos)
	if file == nil {
		return false
	}

	targetPath := typePkg.Path()
	for _, imp := range file.Imports {
		// Skip aliased imports — the generated code uses the canonical package
		// name from types.TypeString, which won't match an alias.
		if imp.Name != nil {
			continue
		}
		path := strings.Trim(imp.Path.Value, `"`)
		if path == targetPath {
			return true
		}
	}
	return false
}

// extractChain walks an expression tree rooted at sliceName[param] and returns
// the chain of field/method accesses after the index expression.
//
// Examples:
//
//	s[i]           → ("",          "i", true)
//	s[i].Name      → (".Name",     "i", true)
//	s[i].Name()    → (".Name()",   "i", true)
//	s[i].F.M()     → (".F.M()",    "i", true)
//	other          → ("",          "",  false)
func extractChain(expr ast.Expr, sliceName string) (chain string, param string, ok bool) {
	switch e := expr.(type) {
	case *ast.IndexExpr:
		ident, isIdent := e.X.(*ast.Ident)
		if !isIdent || ident.Name != sliceName {
			return "", "", false
		}
		idx, isIdent := e.Index.(*ast.Ident)
		if !isIdent {
			return "", "", false
		}
		return "", idx.Name, true

	case *ast.SelectorExpr:
		chain, param, ok := extractChain(e.X, sliceName)
		if !ok {
			return "", "", false
		}
		return chain + "." + e.Sel.Name, param, true

	case *ast.CallExpr:
		if len(e.Args) != 0 {
			return "", "", false
		}
		chain, param, ok := extractChain(e.Fun, sliceName)
		if !ok {
			return "", "", false
		}
		return chain + "()", param, true

	default:
		return "", "", false
	}
}
