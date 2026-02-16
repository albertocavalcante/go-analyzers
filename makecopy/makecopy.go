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

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		block := n.(*ast.BlockStmt)
		if len(block.List) < 2 {
			return
		}

		for i := 0; i < len(block.List)-1; i++ {
			checkPair(pass, block.List[i], block.List[i+1])
		}
	})

	return nil, nil
}

// checkPair checks whether two consecutive statements form a make+copy pattern:
//
//	name := make([]T, len(src))
//	copy(name, src)
func checkPair(pass *analysis.Pass, s1, s2 ast.Stmt) {
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

	// Second arg should be len(src).
	lenCall, ok := makeCall.Args[1].(*ast.CallExpr)
	if !ok {
		return
	}

	lenFun, ok := lenCall.Fun.(*ast.Ident)
	if !ok || lenFun.Name != "len" || len(lenCall.Args) != 1 {
		return
	}

	// Verify it's the builtin len.
	if obj := pass.TypesInfo.ObjectOf(lenFun); obj != nil && obj.Pkg() != nil {
		return // not the builtin
	}

	srcFromLen := lenCall.Args[0]

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

	// Second arg to copy must match the arg to len().
	copySrc := copyCall.Args[1]
	if !sameExpr(pass, copySrc, srcFromLen) {
		return
	}

	srcStr := types.ExprString(copySrc)
	pass.Reportf(assign.Pos(),
		"make+copy can be simplified to %s := slices.Clone(%s)",
		dstIdent.Name, srcStr)
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

	return false
}
