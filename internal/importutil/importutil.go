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
	return AddMultipleImportsEdit(file, []string{pkg})
}

// AddMultipleImportsEdit creates a single TextEdit to add multiple packages to the
// file's imports. Packages that are already imported are skipped. Returns nil if all
// packages are already imported. The pkgs slice should be in the desired order
// (typically alphabetical).
func AddMultipleImportsEdit(file *ast.File, pkgs []string) *analysis.TextEdit {
	// Filter out already-imported packages.
	imported := map[string]bool{}
	for _, imp := range file.Imports {
		imported[imp.Path.Value] = true
	}
	var needed []string
	for _, pkg := range pkgs {
		if !imported[fmt.Sprintf("%q", pkg)] {
			needed = append(needed, pkg)
		}
	}
	if len(needed) == 0 {
		return nil
	}

	// Build insertion text for all needed packages.
	var insertLines string
	for _, pkg := range needed {
		insertLines += fmt.Sprintf("\t%q\n", pkg)
	}

	// Look for an existing import declaration.
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}

		// Grouped import: import ( ... ) — insert before closing paren.
		if gd.Lparen.IsValid() {
			return &analysis.TextEdit{
				Pos:     gd.Rparen,
				End:     gd.Rparen,
				NewText: []byte(insertLines),
			}
		}

		// Single import: import "pkg" or import alias "pkg" — expand to grouped import.
		spec := gd.Specs[0].(*ast.ImportSpec)
		existingImport := spec.Path.Value
		if spec.Name != nil {
			existingImport = spec.Name.Name + " " + existingImport
		}
		return &analysis.TextEdit{
			Pos:     gd.Pos(),
			End:     gd.End(),
			NewText: []byte(fmt.Sprintf("import (\n%s\t%s\n)", insertLines, existingImport)),
		}
	}

	// No import declaration exists — insert after the package clause.
	var newText string
	if len(needed) == 1 {
		newText = fmt.Sprintf("\n\nimport %q", needed[0])
	} else {
		newText = fmt.Sprintf("\n\nimport (\n%s)", insertLines)
	}
	return &analysis.TextEdit{
		Pos:     file.Name.End(),
		End:     file.Name.End(),
		NewText: []byte(newText),
	}
}
