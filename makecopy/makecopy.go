// Package makecopy defines an analyzer that detects make+copy patterns
// that can be replaced with slices.Clone.
//
// # Analyzer makecopy
//
// makecopy: detect make+copy that can be simplified to slices.Clone
//
// This analyzer flags two-statement patterns where a slice is allocated
// with make and immediately populated with copy:
//
//	dst := make([]T, len(src))
//	copy(dst, src)
//
// These can be replaced with the simpler:
//
//	dst := slices.Clone(src)
//
// Available since Go 1.21.
package makecopy

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
	Name:     "makecopy",
	Doc:      "detect make+copy that can be simplified to slices.Clone",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// We look at function bodies: sequences of statements.
	nodeFilter := []ast.Node{
		(*ast.BlockStmt)(nil),
	}

	// Track which files have already received an import TextEdit for "slices"
	// to avoid duplicate edits when multiple diagnostics exist in the same file.
	importEditAdded := map[string]bool{}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		block := n.(*ast.BlockStmt)
		if len(block.List) < 2 {
			return
		}

		for i := 0; i < len(block.List)-1; i++ {
			checkPair(pass, block.List[i], block.List[i+1], importEditAdded)
		}
	})

	return nil, nil
}

// checkPair checks whether two consecutive statements form a make+copy pattern:
//
//	name := make([]T, len(src))
//	copy(name, src)
func checkPair(pass *analysis.Pass, s1, s2 ast.Stmt, importEditAdded map[string]bool) {
	// Statement 1: name := make([]T, len(src))
	assign, ok := s1.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}

	dstIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}

	makeCall, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}

	makeFun, ok := makeCall.Fun.(*ast.Ident)
	if !ok || makeFun.Name != "make" {
		return
	}

	// Verify it's the builtin make.
	if obj := pass.TypesInfo.ObjectOf(makeFun); obj != nil && obj.Pkg() != nil {
		return // not the builtin
	}

	// make must have exactly 2 args: make([]T, len(src))
	if len(makeCall.Args) != 2 {
		return
	}

	// First arg must be a slice type.
	_, ok = makeCall.Args[0].(*ast.ArrayType)
	if !ok {
		return
	}

	// Statement 2: copy(name, src)
	exprStmt, ok := s2.(*ast.ExprStmt)
	if !ok {
		return
	}

	copyCall, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return
	}

	copyFun, ok := copyCall.Fun.(*ast.Ident)
	if !ok || copyFun.Name != "copy" || len(copyCall.Args) != 2 {
		return
	}

	// Verify it's the builtin copy.
	if obj := pass.TypesInfo.ObjectOf(copyFun); obj != nil && obj.Pkg() != nil {
		return // not the builtin
	}

	// First arg to copy must be the same variable as the make target.
	copyDst, ok := copyCall.Args[0].(*ast.Ident)
	if !ok || copyDst.Name != dstIdent.Name {
		return
	}

	// Verify they refer to the same object.
	if pass.TypesInfo.ObjectOf(copyDst) != pass.TypesInfo.ObjectOf(dstIdent) {
		return
	}

	copySrc := copyCall.Args[1]

	// Second arg should be len(src) — check multiple forms.
	if matchLenSource(pass, makeCall.Args[1], copySrc) {
		srcStr := types.ExprString(copySrc)
		msg := fmt.Sprintf("make+copy can be simplified to %s := slices.Clone(%s)",
			dstIdent.Name, srcStr)
		newText := fmt.Sprintf("%s := slices.Clone(%s)", dstIdent.Name, srcStr)

		edits := []analysis.TextEdit{
			{
				Pos:     assign.Pos(),
				End:     s2.End(),
				NewText: []byte(newText),
			},
		}

		// Add "slices" import if not already added for this file.
		file := findFileForPos(pass, assign.Pos())
		fileName := pass.Fset.File(assign.Pos()).Name()
		if file != nil && !importEditAdded[fileName] {
			if ie := addImportEdit(file, "slices"); ie != nil {
				edits = append(edits, *ie)
				importEditAdded[fileName] = true
			}
		}

		pass.Report(analysis.Diagnostic{
			Pos:     assign.Pos(),
			Message: msg,
			SuggestedFixes: []analysis.SuggestedFix{
				{
					Message: msg,
					TextEdits: edits,
				},
			},
		})
	}
}

// matchLenSource reports whether lenArg is a length expression that matches
// copySrc. It handles these forms:
//
//	len(src)         with copy(dst, src)
//	len(src[start:]) with copy(dst, src[start:])
//	len(src)-start   with copy(dst, src[start:])
func matchLenSource(pass *analysis.Pass, lenArg ast.Expr, copySrc ast.Expr) bool {
	// Form 1 & 2: len(x) where x matches copySrc exactly.
	if lenCall, ok := lenArg.(*ast.CallExpr); ok {
		if isBuiltinLen(pass, lenCall) {
			return sameExpr(pass, lenCall.Args[0], copySrc)
		}
	}

	// Form 3: len(base)-idx with copy(dst, base[idx:])
	binExpr, ok := lenArg.(*ast.BinaryExpr)
	if !ok || binExpr.Op != token.SUB {
		return false
	}

	// The LHS must be len(base).
	lenCall, ok := binExpr.X.(*ast.CallExpr)
	if !ok || !isBuiltinLen(pass, lenCall) {
		return false
	}

	// copySrc must be a slice expression base[idx:] (open-ended).
	sliceExpr, ok := copySrc.(*ast.SliceExpr)
	if !ok || sliceExpr.High != nil || sliceExpr.Max != nil {
		return false
	}

	// base in len(base) must match base in base[idx:]
	if !sameExpr(pass, lenCall.Args[0], sliceExpr.X) {
		return false
	}

	// idx in len(base)-idx must match idx in base[idx:]
	return sameExpr(pass, binExpr.Y, sliceExpr.Low)
}

// isBuiltinLen reports whether call is a call to the builtin len with one argument.
func isBuiltinLen(pass *analysis.Pass, call *ast.CallExpr) bool {
	lenFun, ok := call.Fun.(*ast.Ident)
	if !ok || lenFun.Name != "len" || len(call.Args) != 1 {
		return false
	}
	if obj := pass.TypesInfo.ObjectOf(lenFun); obj != nil && obj.Pkg() != nil {
		return false // not the builtin
	}
	return true
}

// sameExpr reports whether two expressions refer to the same thing.
func sameExpr(pass *analysis.Pass, a, b ast.Expr) bool {
	aIdent, aOk := a.(*ast.Ident)
	bIdent, bOk := b.(*ast.Ident)
	if aOk && bOk {
		return pass.TypesInfo.ObjectOf(aIdent) == pass.TypesInfo.ObjectOf(bIdent)
	}

	// Handle selector expressions: x.y == x.y
	aSel, aOk := a.(*ast.SelectorExpr)
	bSel, bOk := b.(*ast.SelectorExpr)
	if aOk && bOk {
		return aSel.Sel.Name == bSel.Sel.Name && sameExpr(pass, aSel.X, bSel.X)
	}

	// Handle slice expressions: x[i:] == x[i:]
	aSlice, aOk := a.(*ast.SliceExpr)
	bSlice, bOk := b.(*ast.SliceExpr)
	if aOk && bOk {
		if !sameExpr(pass, aSlice.X, bSlice.X) {
			return false
		}
		// Both must have same low bound.
		if (aSlice.Low == nil) != (bSlice.Low == nil) {
			return false
		}
		if aSlice.Low != nil && !sameExpr(pass, aSlice.Low, bSlice.Low) {
			return false
		}
		// Both must have same high bound.
		if (aSlice.High == nil) != (bSlice.High == nil) {
			return false
		}
		if aSlice.High != nil && !sameExpr(pass, aSlice.High, bSlice.High) {
			return false
		}
		return true
	}

	// Handle index expressions: x[i] == x[i]
	aIdx, aOk := a.(*ast.IndexExpr)
	bIdx, bOk := b.(*ast.IndexExpr)
	if aOk && bOk {
		return sameExpr(pass, aIdx.X, bIdx.X) && sameExpr(pass, aIdx.Index, bIdx.Index)
	}

	return false
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
