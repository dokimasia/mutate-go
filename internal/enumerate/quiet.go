// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"go/ast"
	"go/token"
	"go/types"

	"go.dokimi.dev/mutate/internal/load"
)

// quietStatements suppresses each compound statement of f that a call
// family suppresses as a whole, with every site inside it: an if, for,
// range, switch, type switch or block statement, labelled or not, whose
// header has no effect and whose bodies contain only statements that call
// families suppress, at least one call among them. Of an if or a switch
// statement whose header has an effect, it suppresses the parts that only
// choose which body runs, as quietSelectors states.
//
// A candidate is an element of a statement list, a labelled statement's
// statement, or an if statement that is the else branch of another. The
// statement's family is the family of its first call in source order that
// a call family suppresses. The walk stops at a suppressed statement, which
// contains every other candidate inside it. The suppressions follow the
// file's call suppressions, so a site inside a call keeps the call's
// family.
func (e *enumerator) quietStatements(f *load.File) {
	listed := map[ast.Stmt]bool{}
	ast.Inspect(f.Syntax, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.BlockStmt:
			for _, st := range s.List {
				listed[st] = true
			}
		case *ast.CaseClause:
			for _, st := range s.Body {
				listed[st] = true
			}
		case *ast.CommClause:
			for _, st := range s.Body {
				listed[st] = true
			}
		case *ast.LabeledStmt:
			listed[s.Stmt] = true
		case *ast.IfStmt:
			if elif, ok := s.Else.(*ast.IfStmt); ok {
				listed[elif] = true
			}
		}
		st, ok := n.(ast.Stmt)
		if !ok || !listed[st] || !compound(st) {
			return true
		}
		if !e.quiet(st) {
			e.quietSelectors(f, st)
			return true
		}
		family := e.firstFamily(st)
		if family == "" {
			return true
		}
		e.suppressions = append(
			e.suppressions,
			suppression{f: f, start: e.off(st.Pos()), end: e.off(st.End()), family: family},
		)
		return false
	})
}

// compound reports whether st is an if, for, range, switch, type switch,
// block or labelled statement.
func compound(st ast.Stmt) bool {
	switch st.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.BlockStmt,
		*ast.LabeledStmt:
		return true
	}
	return false
}

// quiet reports whether call families suppress st as a whole: an
// expression, defer or go statement whose call a call family suppresses,
// an empty statement, or a compound statement whose header has no effect
// and whose bodies contain only quiet statements. Every other statement has
// an effect of its own: a return, a branch, an assignment, a declaration, a
// send, a select and an increment or decrement.
func (e *enumerator) quiet(st ast.Stmt) bool {
	switch s := st.(type) {
	case *ast.ExprStmt, *ast.DeferStmt, *ast.GoStmt:
		call := callOf(st)
		return call != nil && e.calls[call] != ""
	case *ast.EmptyStmt:
		return true
	case *ast.LabeledStmt:
		return e.quiet(s.Stmt)
	case *ast.BlockStmt:
		return e.allQuiet(s.List)
	case *ast.IfStmt:
		return e.initFree(s.Init) && e.effectFree(s.Cond) && e.quiet(s.Body) && (s.Else == nil || e.quiet(s.Else))
	case *ast.ForStmt:
		return e.initFree(s.Init) && (s.Cond == nil || e.effectFree(s.Cond)) && e.postFree(s.Post, s.Init) &&
			e.quiet(s.Body)
	case *ast.RangeStmt:
		return s.Tok != token.ASSIGN && e.rangeFree(s.X) && e.quiet(s.Body)
	case *ast.SwitchStmt:
		if !e.initFree(s.Init) || s.Tag != nil && !e.effectFree(s.Tag) {
			return false
		}
		for _, c := range s.Body.List {
			clause := c.(*ast.CaseClause)
			for _, x := range clause.List {
				if !e.effectFree(x) {
					return false
				}
			}
			if !e.allQuiet(clause.Body) {
				return false
			}
		}
		return true
	case *ast.TypeSwitchStmt:
		if !e.initFree(s.Init) || !e.effectFree(s.Assign) {
			return false
		}
		for _, c := range s.Body.List {
			if !e.allQuiet(c.(*ast.CaseClause).Body) {
				return false
			}
		}
		return true
	}
	return false
}

// allQuiet reports whether every statement of list is quiet.
func (e *enumerator) allQuiet(list []ast.Stmt) bool {
	for _, st := range list {
		if !e.quiet(st) {
			return false
		}
	}
	return true
}

// effectFree reports whether evaluating n calls nothing but conversions,
// the builtins len, cap, min, max, real, imag and complex, and the calls
// that call families suppress, and receives from no channel. A function
// literal is a value, and its body runs only when a call calls it.
func (e *enumerator) effectFree(n ast.Node) bool {
	free := true
	ast.Inspect(n, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			free = free && x.Op != token.ARROW
		case *ast.CallExpr:
			free = free && e.pure(x)
		}
		return free
	})
	return free
}

// pure reports whether call is a conversion, a call of a builtin without an
// effect, or a call that a call family suppresses.
func (e *enumerator) pure(call *ast.CallExpr) bool {
	info := e.pkg.Info
	if e.calls[call] != "" || info.Types[call.Fun].IsType() {
		return true
	}
	id, ok := unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	if _, builtin := info.Uses[id].(*types.Builtin); !builtin {
		return false
	}
	switch id.Name {
	case "len", "cap", "min", "max", "real", "imag", "complex":
		return true
	}
	return false
}

// initFree reports whether an init statement has no effect: it is absent,
// or a short variable declaration whose values have none.
func (e *enumerator) initFree(st ast.Stmt) bool {
	if st == nil {
		return true
	}
	a, ok := st.(*ast.AssignStmt)
	return ok && a.Tok == token.DEFINE && e.effectFree(a)
}

// postFree reports whether a for loop's post statement has no effect
// outside the loop: it is absent, or an increment, a decrement or an
// assignment of variables that the loop's init statement declares, whose
// values have no effect.
func (e *enumerator) postFree(post, init ast.Stmt) bool {
	if post == nil {
		return true
	}
	declared := map[types.Object]bool{}
	if a, ok := init.(*ast.AssignStmt); ok {
		for _, x := range a.Lhs {
			if id, ok := x.(*ast.Ident); ok {
				declared[e.pkg.Info.Defs[id]] = true
			}
		}
	}
	local := func(x ast.Expr) bool {
		id, ok := unparen(x).(*ast.Ident)
		return ok && declared[e.pkg.Info.Uses[id]]
	}
	switch s := post.(type) {
	case *ast.IncDecStmt:
		return local(s.X)
	case *ast.AssignStmt:
		for _, x := range s.Lhs {
			if !local(x) {
				return false
			}
		}
		return e.effectFree(s)
	}
	return false
}

// rangeFree reports whether a range clause over x has no effect: x has
// none, and x is a slice, an array, a pointer to an array, a map, a string
// or an integer. A range over a channel receives, and a range over a
// function calls it.
func (e *enumerator) rangeFree(x ast.Expr) bool {
	switch e.pkg.Info.TypeOf(x).Underlying().(type) {
	case *types.Slice, *types.Array, *types.Pointer, *types.Map, *types.Basic:
		return e.effectFree(x)
	}
	return false
}

// quietSelectors suppresses the parts of an if or a switch statement that
// only choose which of its bodies runs: the condition, the tag and the case
// expressions that have no effect. It does so when every body is quiet and
// contains a call that a call family suppresses, while the statement as a
// whole is not quiet, because a part of its header has an effect. Their
// family is the family of the bodies' first such call in source order. The
// statement's deletion and the parts of its header that have an effect keep
// their mutants. The condition of a loop decides how often its header runs,
// so a loop gets no such suppression.
func (e *enumerator) quietSelectors(f *load.File, st ast.Stmt) {
	var parts []ast.Expr
	var bodies []ast.Stmt
	switch s := st.(type) {
	case *ast.IfStmt:
		parts, bodies = []ast.Expr{s.Cond}, []ast.Stmt{s.Body}
		if s.Else != nil {
			bodies = append(bodies, s.Else)
		}
	case *ast.SwitchStmt:
		if s.Tag != nil {
			parts = append(parts, s.Tag)
		}
		for _, c := range s.Body.List {
			clause := c.(*ast.CaseClause)
			parts, bodies = append(parts, clause.List...), append(bodies, clause.Body...)
		}
	}
	family := ""
	for _, b := range bodies {
		if !e.quiet(b) {
			return
		}
		if family == "" {
			family = e.firstFamily(b)
		}
	}
	if family == "" {
		return
	}
	for _, x := range parts {
		if e.effectFree(x) {
			e.suppressions = append(
				e.suppressions,
				suppression{f: f, start: e.off(x.Pos()), end: e.off(x.End()), family: family},
			)
		}
	}
}

// firstFamily returns the family of the first call in st, in source order,
// that a call family suppresses, or "" when st has none.
func (e *enumerator) firstFamily(st ast.Stmt) string {
	family := ""
	ast.Inspect(st, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && family == "" {
			family = e.calls[call]
		}
		return family == ""
	})
	return family
}
