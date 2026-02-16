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
// Available since Go 1.21.
package sortmigrate

import (
	"go/ast"
	"go/types"

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
	"Strings":            "slices.Sort",
	"Ints":               "slices.Sort",
	"Float64s":           "slices.Sort",
	"Slice":              "slices.SortFunc",
	"SliceStable":        "slices.SortStableFunc",
	"SliceIsSorted":      "slices.IsSortedFunc",
	"IntsAreSorted":      "slices.IsSorted",
	"StringsAreSorted":   "slices.IsSorted",
	"Float64sAreSorted":  "slices.IsSorted",
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
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

		pass.Reportf(call.Pos(),
			"sort.%s can be replaced with %s",
			funcName, replacement)
	})

	return nil, nil
}
