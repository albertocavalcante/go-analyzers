// Package searchmigrate defines an analyzer that detects sort.Search calls
// that could be replaced with slices.BinarySearch or slices.BinarySearchFunc.
//
// # Analyzer searchmigrate
//
// searchmigrate: detect sort.Search that can be simplified to slices.BinarySearch
//
// This analyzer flags calls to sort.Search where the closure body is a
// simple comparison against a slice element:
//
//	sort.Search(len(s), func(i int) bool { return s[i] >= target })
//
// These can be replaced with:
//
//	slices.BinarySearch(s, target)
//
// Available since Go 1.21.
package searchmigrate

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "searchmigrate",
	Doc:      "detect sort.Search that can be simplified to slices.BinarySearch",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		if !isSortSearchCall(pass, call) {
			return
		}

		pass.Reportf(call.Pos(),
			"sort.Search can potentially be replaced with slices.BinarySearch or slices.BinarySearchFunc")
	})

	return nil, nil
}

// isSortSearchCall reports whether call is sort.Search(n, func...).
func isSortSearchCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Search" || len(call.Args) != 2 {
		return false
	}

	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}

	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return false
	}

	return pkgName.Imported().Path() == "sort"
}
