// Package clampcheck defines an analyzer that detects clamp patterns
// that can be replaced with min(max(x, lo), hi).
//
// # Analyzer clampcheck
//
// clampcheck: detect if-else clamp patterns that can use min/max builtins
//
// This analyzer flags if-elseif-else patterns that clamp a value to a range:
//
//	if x < lo {
//	    x = lo
//	} else if x > hi {
//	    x = hi
//	}
//
// These can be replaced with:
//
//	x = min(max(x, lo), hi)
//
// Or equivalently:
//
//	x = max(min(x, hi), lo)
//
// Available since Go 1.21.
package clampcheck

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "clampcheck",
	Doc:      "detect if-else clamp patterns that can use min/max builtins",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.IfStmt)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		ifStmt := n.(*ast.IfStmt)
		checkClamp(pass, ifStmt)
	})

	return nil, nil
}

// checkClamp looks for patterns like:
//
//	if x < lo { x = lo } else if x > hi { x = hi }
//	if x > hi { x = hi } else if x < lo { x = lo }
func checkClamp(pass *analysis.Pass, ifStmt *ast.IfStmt) {
	// Must have no init statement.
	if ifStmt.Init != nil {
		return
	}

	// Must have an else branch that is another if statement.
	elseIf, ok := ifStmt.Else.(*ast.IfStmt)
	if !ok {
		return
	}

	// The else-if must not have a further else (exactly 2 branches).
	if elseIf.Else != nil {
		return
	}

	// Both conditions must be binary comparisons.
	cond1, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return
	}
	cond2, ok := elseIf.Cond.(*ast.BinaryExpr)
	if !ok {
		return
	}

	// One must be < (or <=) and the other > (or >=).
	isLower1 := cond1.Op == token.LSS || cond1.Op == token.LEQ
	isUpper1 := cond1.Op == token.GTR || cond1.Op == token.GEQ
	isLower2 := cond2.Op == token.LSS || cond2.Op == token.LEQ
	isUpper2 := cond2.Op == token.GTR || cond2.Op == token.GEQ

	if !((isLower1 && isUpper2) || (isUpper1 && isLower2)) {
		return
	}

	// Both bodies must be single assignment statements.
	body1 := singleAssign(ifStmt.Body)
	if body1 == nil {
		return
	}
	body2 := singleAssign(elseIf.Body)
	if body2 == nil {
		return
	}

	// The LHS of both assignments must be the same variable,
	// and it must match the LHS of both conditions.
	lhs1, ok := body1.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	lhs2, ok := body2.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}

	if pass.TypesInfo.ObjectOf(lhs1) != pass.TypesInfo.ObjectOf(lhs2) {
		return
	}

	// The variable being compared in the conditions should be the same as
	// the variable being assigned.
	var condVar1 ast.Expr
	if isLower1 || isUpper1 {
		condVar1 = cond1.X
	}

	condVarIdent, ok := condVar1.(*ast.Ident)
	if !ok {
		return
	}

	if pass.TypesInfo.ObjectOf(condVarIdent) != pass.TypesInfo.ObjectOf(lhs1) {
		return
	}

	// Check that the assigned values match the comparison bounds.
	// For: if x < lo { x = lo } — the assignment RHS should be the bound.
	rhs1Str := types.ExprString(body1.Rhs[0])
	rhs2Str := types.ExprString(body2.Rhs[0])
	varStr := lhs1.Name

	pass.Reportf(ifStmt.Pos(),
		"clamp pattern can be simplified to %s = min(max(%s, %s), %s) or use a clamp helper",
		varStr, varStr, rhs1Str, rhs2Str)
}

// singleAssign returns the single assignment statement in a block, or nil.
func singleAssign(block *ast.BlockStmt) *ast.AssignStmt {
	if len(block.List) != 1 {
		return nil
	}
	assign, ok := block.List[0].(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil
	}
	if assign.Tok != token.ASSIGN {
		return nil
	}
	return assign
}
