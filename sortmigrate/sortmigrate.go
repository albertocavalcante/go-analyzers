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
	"fmt"
	"go/ast"
	"go/token"
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

// unsafeAutofix lists sort functions whose callback signatures are incompatible
// with their slices equivalents, so auto-fix should not be offered.
var unsafeAutofix = map[string]bool{
	"Slice":         true,
	"SliceStable":   true,
	"SliceIsSorted": true,
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	// Track which files have already received an import TextEdit for "slices"
	// to avoid duplicate edits when multiple diagnostics exist in the same file.
	importEditAdded := map[string]bool{}

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

		msg := fmt.Sprintf("sort.%s can be replaced with %s", funcName, replacement)

		diag := analysis.Diagnostic{
			Pos:     call.Pos(),
			Message: msg,
		}

		// sort.Slice, sort.SliceStable, and sort.SliceIsSorted have incompatible
		// callback signatures with their slices equivalents (func(i, j int) bool
		// vs func(a, b T) int). Only provide auto-fix for safe, direct replacements.
		if !unsafeAutofix[funcName] {
			edits := []analysis.TextEdit{
				{
					Pos:     sel.Pos(),
					End:     sel.Sel.End(),
					NewText: []byte(replacement),
				},
			}

			// Add "slices" import if not already added for this file.
			file := findFileForPos(pass, call.Pos())
			fileName := pass.Fset.File(call.Pos()).Name()
			if file != nil && !importEditAdded[fileName] {
				if ie := addImportEdit(file, "slices"); ie != nil {
					edits = append(edits, *ie)
					importEditAdded[fileName] = true
				}
			}

			diag.SuggestedFixes = []analysis.SuggestedFix{
				{
					Message: msg,
					TextEdits: edits,
				},
			}
		}

		pass.Report(diag)
	})

	return nil, nil
}

// findFileForPos returns the *ast.File that contains the given position.
func findFileForPos(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, f := range pass.Files {
		if pass.Fset.File(f.Pos()).Name() == pass.Fset.File(pos).Name() {
			return f
		}
	}
	return nil
}

// addImportEdit creates a TextEdit to add the given package to the file's imports.
// It returns nil if the package is already imported.
func addImportEdit(file *ast.File, pkg string) *analysis.TextEdit {
	quotedPkg := fmt.Sprintf("%q", pkg)

	// Check if already imported.
	for _, imp := range file.Imports {
		if imp.Path.Value == quotedPkg {
			return nil
		}
	}

	// Look for an existing import declaration.
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}

		// Grouped import: import ( ... )
		if gd.Lparen.IsValid() {
			return &analysis.TextEdit{
				Pos:     gd.Rparen,
				End:     gd.Rparen,
				NewText: []byte(fmt.Sprintf("\t%s\n", quotedPkg)),
			}
		}

		// Single import: import "pkg" — replace with grouped import including new pkg.
		// The existing import spec is gd.Specs[0].
		existingImport := gd.Specs[0].(*ast.ImportSpec).Path.Value
		return &analysis.TextEdit{
			Pos:     gd.Pos(),
			End:     gd.End(),
			NewText: []byte(fmt.Sprintf("import (\n\t%s\n\t%s\n)", quotedPkg, existingImport)),
		}
	}

	// No import declaration exists — insert after the package clause.
	return &analysis.TextEdit{
		Pos:     file.Name.End(),
		End:     file.Name.End(),
		NewText: []byte(fmt.Sprintf("\n\nimport %s", quotedPkg)),
	}
}
