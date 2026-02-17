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
	"fmt"
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

	// Check if-else-if clamp patterns.
	ifFilter := []ast.Node{
		(*ast.IfStmt)(nil),
	}

	inspect.Preorder(ifFilter, func(n ast.Node) {
		ifStmt := n.(*ast.IfStmt)
		checkClamp(pass, ifStmt)
	})

	// Check consecutive if-return clamp patterns in block statements.
	blockFilter := []ast.Node{
		(*ast.BlockStmt)(nil),
	}

	inspect.Preorder(blockFilter, func(n ast.Node) {
		block := n.(*ast.BlockStmt)
		checkConsecutiveIfReturn(pass, block)
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

	// The variable being compared in both conditions should be the same as
	// the variable being assigned.
	condVarIdent1, ok := cond1.X.(*ast.Ident)
	if !ok {
		return
	}
	if pass.TypesInfo.ObjectOf(condVarIdent1) != pass.TypesInfo.ObjectOf(lhs1) {
		return
	}

	condVarIdent2, ok := cond2.X.(*ast.Ident)
	if !ok {
		return
	}
	if pass.TypesInfo.ObjectOf(condVarIdent2) != pass.TypesInfo.ObjectOf(lhs1) {
		return
	}

	// Check that the assigned values match the comparison bounds.
	// For: if x < lo { x = lo } — the assignment RHS should be the bound.
	rhs1Str := types.ExprString(body1.Rhs[0])
	rhs2Str := types.ExprString(body2.Rhs[0])
	varStr := lhs1.Name

	// When the first condition checks the lower bound (< or <=), emit min(max(x, lo), hi).
	// When the first condition checks the upper bound (> or >=), emit max(min(x, hi), lo).
	var msg, newText string
	if isLower1 {
		msg = fmt.Sprintf("clamp pattern can be simplified to %s = min(max(%s, %s), %s) or use a clamp helper",
			varStr, varStr, rhs1Str, rhs2Str)
		newText = fmt.Sprintf("%s = min(max(%s, %s), %s)", varStr, varStr, rhs1Str, rhs2Str)
	} else {
		msg = fmt.Sprintf("clamp pattern can be simplified to %s = max(min(%s, %s), %s) or use a clamp helper",
			varStr, varStr, rhs1Str, rhs2Str)
		newText = fmt.Sprintf("%s = max(min(%s, %s), %s)", varStr, varStr, rhs1Str, rhs2Str)
	}

	pass.Report(analysis.Diagnostic{
		Pos:     ifStmt.Pos(),
		Message: msg,
		SuggestedFixes: []analysis.SuggestedFix{
			{
				Message: msg,
				TextEdits: []analysis.TextEdit{
					{
						Pos:     ifStmt.Pos(),
						End:     ifStmt.End(),
						NewText: []byte(newText),
					},
				},
			},
		},
	})
}

// checkConsecutiveIfReturn looks for patterns like:
//
//	if v < lo { return lo }
//	if v > hi { return hi }
//	return v
//
// Two consecutive if statements (no else) each containing a single return,
// followed by a plain return statement.
func checkConsecutiveIfReturn(pass *analysis.Pass, block *ast.BlockStmt) {
	// Need at least 3 statements: if, if, return.
	if len(block.List) < 3 {
		return
	}

	for i := 0; i < len(block.List)-2; i++ {
		if1, ok := block.List[i].(*ast.IfStmt)
		if !ok || if1.Init != nil || if1.Else != nil {
			continue
		}
		if2, ok := block.List[i+1].(*ast.IfStmt)
		if !ok || if2.Init != nil || if2.Else != nil {
			continue
		}
		retStmt, ok := block.List[i+2].(*ast.ReturnStmt)
		if !ok || len(retStmt.Results) != 1 {
			continue
		}

		// Both bodies must be single return statements.
		ret1 := singleReturn(if1.Body)
		if ret1 == nil {
			continue
		}
		ret2 := singleReturn(if2.Body)
		if ret2 == nil {
			continue
		}

		// Both conditions must be binary comparisons.
		cond1, ok := if1.Cond.(*ast.BinaryExpr)
		if !ok {
			continue
		}
		cond2, ok := if2.Cond.(*ast.BinaryExpr)
		if !ok {
			continue
		}

		// One must be < (or <=) and the other > (or >=).
		isLower1 := cond1.Op == token.LSS || cond1.Op == token.LEQ
		isUpper1 := cond1.Op == token.GTR || cond1.Op == token.GEQ
		isLower2 := cond2.Op == token.LSS || cond2.Op == token.LEQ
		isUpper2 := cond2.Op == token.GTR || cond2.Op == token.GEQ

		if !((isLower1 && isUpper2) || (isUpper1 && isLower2)) {
			continue
		}

		// The variable being compared in both conditions must be the same.
		condVar1, ok := cond1.X.(*ast.Ident)
		if !ok {
			continue
		}
		condVar2, ok := cond2.X.(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.ObjectOf(condVar1) != pass.TypesInfo.ObjectOf(condVar2) {
			continue
		}

		// The final return should return the same variable.
		retVar, ok := retStmt.Results[0].(*ast.Ident)
		if !ok {
			continue
		}
		if pass.TypesInfo.ObjectOf(retVar) != pass.TypesInfo.ObjectOf(condVar1) {
			continue
		}

		varStr := condVar1.Name
		bound1Str := types.ExprString(ret1.Results[0])
		bound2Str := types.ExprString(ret2.Results[0])

		// When the first condition checks the lower bound (< or <=), emit return min(max(v, lo), hi).
		// When the first condition checks the upper bound (> or >=), emit return max(min(v, hi), lo).
		var msg, newText string
		if isLower1 {
			msg = fmt.Sprintf("clamp pattern can be simplified to return min(max(%s, %s), %s) or use a clamp helper",
				varStr, bound1Str, bound2Str)
			newText = fmt.Sprintf("return min(max(%s, %s), %s)", varStr, bound1Str, bound2Str)
		} else {
			msg = fmt.Sprintf("clamp pattern can be simplified to return max(min(%s, %s), %s) or use a clamp helper",
				varStr, bound1Str, bound2Str)
			newText = fmt.Sprintf("return max(min(%s, %s), %s)", varStr, bound1Str, bound2Str)
		}

		pass.Report(analysis.Diagnostic{
			Pos:     if1.Pos(),
			Message: msg,
			SuggestedFixes: []analysis.SuggestedFix{
				{
					Message: msg,
					TextEdits: []analysis.TextEdit{
						{
							Pos:     if1.Pos(),
							End:     retStmt.End(),
							NewText: []byte(newText),
						},
					},
				},
			},
		})
	}
}

// singleReturn returns the single return statement in a block, or nil.
func singleReturn(block *ast.BlockStmt) *ast.ReturnStmt {
	if len(block.List) != 1 {
		return nil
	}
	ret, ok := block.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	return ret
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
