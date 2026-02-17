// Package importutil provides shared helpers for adding import TextEdits
// in go/analysis SuggestedFixes.
package importutil

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// FindFileForPos returns the *ast.File that contains the given position.
func FindFileForPos(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, f := range pass.Files {
		if pass.Fset.File(f.Pos()).Name() == pass.Fset.File(pos).Name() {
			return f
		}
	}
	return nil
}

// AddImportEdit creates a TextEdit to add the given package to the file's imports.
// It returns nil if the package is already imported.
func AddImportEdit(file *ast.File, pkg string) *analysis.TextEdit {
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

		// Single import: import "pkg" or import alias "pkg" — replace with grouped import.
		spec := gd.Specs[0].(*ast.ImportSpec)
		existingImport := spec.Path.Value
		if spec.Name != nil {
			existingImport = spec.Name.Name + " " + existingImport
		}
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
