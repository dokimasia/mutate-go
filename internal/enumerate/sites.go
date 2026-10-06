// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// The builtins whose calls the zero-value rule and the terminating-statement
// rule read.
const (
	builtinNew   = "new"
	builtinPanic = "panic"
)

// frame is one node on the path from the walk's root to the visited node,
// with the innermost function type around it.
type frame struct {
	node ast.Node
	fn   *ast.FuncType
}

// walk visits root and every node below it. scope is the scope of every
// site that it finds, and fn the function type around root, or nil.
func (e *enumerator) walk(f *load.File, root ast.Node, scope string, fn *ast.FuncType) {
	var stack []frame
	constants := 0
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if e.constantSite(top.node) {
				constants--
			}
			return false
		}
		inner := fn
		if len(stack) > 0 {
			inner = stack[len(stack)-1].fn
		}
		// outer is n with the parentheses around it, and around the closest
		// node around outer, or nil at the root. A negation reads the
		// position of n by them, so parentheses do not change it. No
		// statement is in parentheses, so around is a statement's parent.
		var around ast.Node
		outer := n
		for _, fr := range slices.Backward(stack) {
			if _, ok := fr.node.(*ast.ParenExpr); !ok {
				around = fr.node
				break
			}
			outer = fr.node
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			inner = lit.Type
		}
		if call, ok := n.(*ast.CallExpr); ok {
			e.family(f, call)
		}
		if e.constantSite(n) {
			if constants == 0 {
				e.addSkip(f, n, spec.SkipConstant)
			}
			constants++
		}
		if constants == 0 {
			e.visit(f, n, outer, around, scope, inner)
		}
		stack = append(stack, frame{node: n, fn: inner})
		return true
	})
}

// constantSite reports whether n is a constant expression of a catalogue
// class: a comparison, a connector, or arithmetic on numbers.
func (e *enumerator) constantSite(n ast.Node) bool {
	b, ok := n.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	tv := e.pkg.Info.Types[b]
	if tv.Value == nil {
		return false
	}
	switch b.Op {
	case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ, token.LAND, token.LOR:
		return true
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
		return isNumber(tv.Type)
	default:
		return false
	}
}

// visit makes the sites of n. outer and around are the position of n
// without its parentheses, as walk states them, and fn is the innermost
// function type around n.
func (e *enumerator) visit(f *load.File, n, outer, around ast.Node, scope string, fn *ast.FuncType) {
	switch x := n.(type) {
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND, token.LOR:
			e.connector(f, x, scope)
		case token.EQL, token.NEQ:
			e.equality(f, x, scope)
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
			e.ordered(f, x, scope)
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
			e.arithmetic(f, x, scope)
		default:
			// The catalogue has no kind of the other operators, such as a
			// shift or a bitwise operator.
		}
	case *ast.AssignStmt:
		e.compound(f, x, scope)
		e.delete(f, x, around, scope)
	case *ast.IncDecStmt:
		e.incdec(f, x, around, scope)
	case *ast.ReturnStmt:
		e.zero(f, x, fn, scope)
	case ast.Stmt:
		e.delete(f, x, around, scope)
	case *ast.UnaryExpr:
		if x.Op == token.SUB {
			e.minus(f, x, scope)
		}
		e.not(f, x, outer.(ast.Expr), around, scope)
	case ast.Expr:
		e.not(f, x, outer.(ast.Expr), around, scope)
	}
}

func (e *enumerator) newSite(f *load.File, n ast.Node, scope string, form Form, op token.Token) *Site {
	s := &Site{File: f, Scope: scope, Start: e.off(n.Pos()), End: e.off(n.End()), Form: form, Node: n, Op: op}
	e.sites = append(e.sites, s)
	return s
}

func (e *enumerator) addSkip(f *load.File, n ast.Node, reason spec.SkipReason) {
	e.skips = append(e.skips, skip{f: f, start: e.off(n.Pos()), end: e.off(n.End()), reason: reason})
}

// add makes a mutant of kind at s, which writes op in place of the site's
// operator, and whose source is replacement.
func (s *Site) add(kind spec.Kind, op token.Token, replacement string) *Mutant {
	m := &Mutant{Site: s, Kind: kind, Op: op, Replacement: replacement}
	s.Mutants = append(s.Mutants, m)
	return m
}

var (
	arith = map[token.Token]token.Token{
		token.ADD: token.SUB,
		token.SUB: token.ADD,
		token.MUL: token.QUO,
		token.QUO: token.MUL,
		token.REM: token.MUL,
	}
	boundary = map[token.Token]token.Token{
		token.LSS: token.LEQ,
		token.LEQ: token.LSS,
		token.GTR: token.GEQ,
		token.GEQ: token.GTR,
	}
	// assign maps a compound assignment's operator to its binary operator.
	assign = map[token.Token]token.Token{
		token.ADD_ASSIGN: token.ADD,
		token.SUB_ASSIGN: token.SUB,
		token.MUL_ASSIGN: token.MUL,
		token.QUO_ASSIGN: token.QUO,
		token.REM_ASSIGN: token.REM,
	}
)

// divisionByZero is the compiler's message for a division of an integer by
// a constant 0, the reason of an aor mutant that writes one.
const divisionByZero = "invalid operation: division by zero"

// swapped returns the site's source with the operator text at opPos, of
// length n, replaced by to.
func (e *enumerator) swapped(f *load.File, s *Site, opPos token.Pos, n int, to string) string {
	at := e.off(opPos)
	return string(f.Text[s.Start:at]) + to + string(f.Text[at+n:s.End])
}

// equality makes the mutants of == and !=: true and false. Every such
// comparison is a site, whatever its operands' types, because each of its
// kinds needs only the comparison's result. The walk skips a comparison
// whose value is a constant.
func (e *enumerator) equality(f *load.File, n *ast.BinaryExpr, scope string) {
	if !isPlainBool(e.pkg.Info.TypeOf(n)) {
		e.addSkip(f, n, spec.SkipNamedBool)
		return
	}
	s := e.newSite(f, n, scope, Equality, n.Op)
	s.add(spec.RORTrue, token.ILLEGAL, "true")
	s.add(spec.RORFalse, token.ILLEGAL, "false")
}

// ordered makes the mutants of <, <=, > and >=: the boundary, and false for
// < and > or true for <= and >=. The constant that the catalogue leaves out
// differs from the original wherever the boundary or the kept constant
// does, so they subsume it.
func (e *enumerator) ordered(f *load.File, n *ast.BinaryExpr, scope string) {
	info := e.pkg.Info
	operand := info.TypeOf(n.X)
	switch {
	case !isPlainBool(info.TypeOf(n)):
		e.addSkip(f, n, spec.SkipNamedBool)
		return
	case isTypeParam(operand):
		e.addSkip(f, n, spec.SkipTypeParameter)
		return
	case e.contextShift(operand, n):
		e.addSkip(f, n, spec.SkipContextShift)
		return
	}
	s := e.newSite(f, n, scope, Ordered, n.Op)
	s.TypeArg = e.typeArg(operand, n.Pos())
	b := boundary[n.Op]
	s.add(spec.RORBoundary, b, e.swapped(f, s, n.OpPos, len(n.Op.String()), b.String()))
	if n.Op == token.LEQ || n.Op == token.GEQ {
		s.add(spec.RORTrue, token.ILLEGAL, "true")
	} else {
		s.add(spec.RORFalse, token.ILLEGAL, "false")
	}
}

// arithmetic makes the mutant of +, -, *, / and % on numbers. The package
// type-checks, so both operands and the result have one type. Arithmetic on
// numbers whose value is a constant is a constant site, which the walk
// skips. A mutant that divides an integer by a constant 0 is not viable,
// with the compiler's message as its reason.
func (e *enumerator) arithmetic(f *load.File, n *ast.BinaryExpr, scope string) {
	operand := e.pkg.Info.TypeOf(n.X)
	if !isNumber(operand) {
		return
	}
	switch {
	case isTypeParam(operand):
		e.addSkip(f, n, spec.SkipTypeParameter)
		return
	case e.contextShift(operand, n):
		e.addSkip(f, n, spec.SkipContextShift)
		return
	}
	s := e.newSite(f, n, scope, Arithmetic, n.Op)
	s.TypeArg = e.typeArg(operand, n.Pos())
	to := arith[n.Op]
	m := s.add(spec.AOR, to, e.swapped(f, s, n.OpPos, len(n.Op.String()), to.String()))
	if to == token.QUO && zeroDivisor(e.pkg.Info, operand, n.Y) {
		m.Status, m.Reason = NotViable, divisionByZero
	}
}

// zeroDivisor reports whether y is a constant whose value is 0 and t an
// integer type, so that the compiler rejects a division by y.
func zeroDivisor(info *types.Info, t types.Type, y ast.Expr) bool {
	b, ok := t.Underlying().(*types.Basic)
	v := info.Types[y].Value
	return ok && b.Info()&types.IsInteger != 0 && v != nil && constant.Sign(v) == 0
}

// contextShift reports whether an operand of n contains a non-constant
// shift of an untyped constant while the operands' type has no name that
// resolves in the package. Such a constant takes its type from the
// expression around it.
func (e *enumerator) contextShift(operand types.Type, n *ast.BinaryExpr) bool {
	if _, ok := e.typeName(operand); ok {
		return false
	}
	return e.hasContextShift(n.X) || e.hasContextShift(n.Y)
}

// connector makes the mutants of && and ||: each operand alone, and false
// for && or true for ||. A mutant does not evaluate the operands that it
// leaves out. The connector's operands get no negation, because its mutants
// subsume a negated operand.
func (e *enumerator) connector(f *load.File, n *ast.BinaryExpr, scope string) {
	if !isPlainBool(e.pkg.Info.TypeOf(n)) {
		e.addSkip(f, n, spec.SkipNamedBool)
		return
	}
	e.operands[ast.Unparen(n.X)] = true
	e.operands[ast.Unparen(n.Y)] = true
	s := e.newSite(f, n, scope, Connector, n.Op)
	s.add(spec.LCRLeft, token.ILLEGAL, e.text(f, n.X))
	s.add(spec.LCRRight, token.ILLEGAL, e.text(f, n.Y))
	kind, value := spec.LCRFalse, "false"
	if n.Op == token.LOR {
		kind, value = spec.LCRTrue, "true"
	}
	s.add(kind, token.ILLEGAL, value)
}

// compound makes the mutant of +=, -=, *=, /= and %= on one number. The
// package type-checks, so such an assignment has one target. A mutant that
// divides an integer by a constant 0 is not viable, as arithmetic states.
func (e *enumerator) compound(f *load.File, n *ast.AssignStmt, scope string) {
	op, ok := assign[n.Tok]
	if !ok {
		return
	}
	t := e.pkg.Info.TypeOf(n.Lhs[0])
	if !isNumber(t) {
		return
	}
	switch {
	case isTypeParam(t):
		e.addSkip(f, n, spec.SkipTypeParameter)
		return
	case !sideEffectFree(n.Lhs[0]):
		e.addSkip(f, n, spec.SkipSideEffects)
		return
	}
	s := e.newSite(f, n, scope, Compound, op)
	s.TypeArg = e.typeArg(t, n.Pos())
	to := arith[op]
	m := s.add(spec.AOR, to, e.swapped(f, s, n.TokPos, len(n.Tok.String()), to.String()+"="))
	if to == token.QUO && zeroDivisor(e.pkg.Info, t, n.Rhs[0]) {
		m.Status, m.Reason = NotViable, divisionByZero
	}
}

// incdec makes the mutant of an increment or decrement statement in a
// statement list or in a for loop's post statement.
func (e *enumerator) incdec(f *load.File, n *ast.IncDecStmt, parent ast.Node, scope string) {
	t := e.pkg.Info.TypeOf(n.X)
	if !isNumber(t) {
		return
	}
	form := IncDec
	switch p := parent.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.LabeledStmt:
	case *ast.ForStmt:
		if p.Post != n {
			return
		}
		switch {
		case isTypeParam(t):
			e.addSkip(f, n, spec.SkipTypeParameter)
			return
		case !sideEffectFree(n.X):
			e.addSkip(f, n, spec.SkipSideEffects)
			return
		}
		form = IncDecPost
	default:
		return
	}
	to := token.DEC
	if n.Tok == token.DEC {
		to = token.INC
	}
	s := e.newSite(f, n, scope, form, n.Tok)
	if form == IncDecPost {
		s.TypeArg = e.typeArg(t, n.Pos())
	}
	s.add(spec.UOIIncDec, to, e.swapped(f, s, n.TokPos, len(n.Tok.String()), to.String()))
}

// not negates a boolean operand: an identifier, a selector, a call, an
// index, a type assertion, a dereference, or a !x, whose mutant is x. An
// operand of a connector that is a site gets no negation. outer is x with
// the parentheses around it, and around the closest node around outer, so
// an operand in parentheses has the position of the operand without them.
func (e *enumerator) not(f *load.File, x, outer ast.Expr, around ast.Node, scope string) {
	if e.operands[x] {
		return
	}
	var operand ast.Expr
	switch n := x.(type) {
	case *ast.Ident,
		*ast.SelectorExpr,
		*ast.CallExpr,
		*ast.IndexExpr,
		*ast.IndexListExpr,
		*ast.TypeAssertExpr,
		*ast.StarExpr:
	case *ast.UnaryExpr:
		if n.Op != token.NOT {
			return
		}
		operand = n.X
	default:
		return
	}
	// The type checker records the source of a comma-ok assignment as a
	// tuple, so the bool test excludes it.
	tv, ok := e.pkg.Info.Types[x]
	if !ok || tv.Value != nil || !tv.IsValue() || !isPlainBool(tv.Type) || !valuePosition(around, outer) {
		return
	}
	s := e.newSite(f, x, scope, Not, token.ILLEGAL)
	replacement := "!" + e.text(f, x)
	if operand != nil {
		replacement = e.text(f, operand)
	}
	s.add(spec.UOINot, token.ILLEGAL, replacement)
}

// valuePosition reports whether outer, a boolean value with the parentheses
// around it, is in a position where a negation may replace it: not the
// target of an assignment, not the operand of & or !, not a composite
// literal's key, and not the expression of an increment, an expression
// statement, a deferred call, a go statement or a range clause. around is
// the closest node around outer. The type checker records no type for a
// selector's name, and a called function is no boolean, so valuePosition
// tests neither.
func valuePosition(around ast.Node, outer ast.Expr) bool {
	switch p := around.(type) {
	case *ast.AssignStmt:
		return !slices.Contains(p.Lhs, outer)
	case *ast.UnaryExpr:
		return p.Op != token.AND && p.Op != token.NOT
	case *ast.KeyValueExpr:
		return p.Key != outer
	case *ast.IncDecStmt, *ast.ExprStmt, *ast.DeferStmt, *ast.GoStmt, *ast.RangeStmt:
		return false
	}
	return true
}

// minus makes the mutant of a unary minus whose value is not a constant.
func (e *enumerator) minus(f *load.File, n *ast.UnaryExpr, scope string) {
	tv := e.pkg.Info.Types[n]
	if tv.Value != nil || !isNumber(tv.Type) {
		return
	}
	if isTypeParam(tv.Type) {
		e.addSkip(f, n, spec.SkipTypeParameter)
		return
	}
	s := e.newSite(f, n, scope, Minus, token.SUB)
	s.TypeArg = e.typeArg(tv.Type, n.Pos())
	s.add(spec.UOIMinus, token.ILLEGAL, e.text(f, n.X))
}

// delete removes one statement of a statement list. It keeps declarations,
// branches, returns, increments and decrements, every statement that
// contains a label, and the list's final statement that is not empty when
// that statement is terminating.
//
// A statement whose parent is a block, a case clause or a communication
// clause is an element of the parent's list, except the communication of a
// select statement's clause, so delete finds the list from the parent that
// the walk passes, without a search of the list.
func (e *enumerator) delete(f *load.File, st ast.Stmt, parent ast.Node, scope string) {
	var list []ast.Stmt
	switch p := parent.(type) {
	case *ast.BlockStmt:
		list = p.List
	case *ast.CaseClause:
		list = p.Body
	case *ast.CommClause:
		if p.Comm == st {
			return
		}
		list = p.Body
	default:
		return
	}
	switch s := st.(type) {
	case *ast.ExprStmt, *ast.SendStmt, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt,
		*ast.TypeSwitchStmt, *ast.SelectStmt, *ast.BlockStmt, *ast.DeferStmt, *ast.GoStmt:
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE || allBlank(s.Lhs) {
			return
		}
	default:
		return
	}
	if hasLabel(st) || final(list) == st && e.isTerminating(st) {
		return
	}
	s := e.newSite(f, st, scope, Delete, token.ILLEGAL)
	s.add(spec.SBRDelete, token.ILLEGAL, "")
}

// zero makes the mutant that returns the zero value of every result type
// before a return statement whose results are not all the zero values of
// their result types already. The mutant does not evaluate the results.
func (e *enumerator) zero(f *load.File, n *ast.ReturnStmt, fn *ast.FuncType, scope string) {
	if fn == nil || fn.Results == nil || len(n.Results) == 0 {
		return
	}
	var zeros []string
	var dests []types.Type
	for _, field := range fn.Results.List {
		zero, dest := e.zeroOf(f, field.Type), e.pkg.Info.TypeOf(field.Type)
		for range max(len(field.Names), 1) {
			zeros, dests = append(zeros, zero), append(dests, dest)
		}
	}
	if e.allZero(n.Results, dests) {
		return
	}
	s := e.newSite(f, n, scope, Zero, token.ILLEGAL)
	s.Zeros = zeros
	s.add(spec.SBRZero, token.ILLEGAL, "return "+strings.Join(zeros, ", "))
}

// zeroOf returns the zero value of the type that the expression t writes,
// as Go code writes it: 0, "", false or nil by the underlying type, the
// type followed by {} for a struct or an array, and *new(T) for a type
// parameter T, which has no literal.
func (e *enumerator) zeroOf(f *load.File, t ast.Expr) string {
	typ := e.pkg.Info.TypeOf(t)
	text := e.text(f, ast.Unparen(t))
	if isTypeParam(typ) {
		return "*" + builtinNew + "(" + text + ")"
	}
	switch u := typ.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsNumeric != 0:
			return "0"
		case u.Info()&types.IsString != 0:
			return `""`
		case u.Info()&types.IsBoolean != 0:
			return "false"
		}
	case *types.Struct, *types.Array:
		return text + "{}"
	}
	return "nil"
}

// allZero reports whether every result is the zero value of its result
// type, of dests in order. A return of the results of one call has fewer
// results than result types, and is no zero value.
func (e *enumerator) allZero(results []ast.Expr, dests []types.Type) bool {
	if len(results) != len(dests) {
		return false
	}
	for i, x := range results {
		if !e.isZero(x, dests[i]) {
			return false
		}
	}
	return true
}

// isZero reports whether x is the zero value of dest, the type of the
// result or the element that x gives: nil, a constant whose value is 0, ""
// or false, *new(T) of a type T, a variable that unwrittenVariables
// returns, or a composite literal of a struct or an array type whose every
// element is the zero value of the element's type. A value of a type that
// is no interface gives an interface that is not nil, so of an interface
// type only nil, *new(T) of an interface type T and such a variable of an
// interface type are the zero value. A composite literal of a slice or a
// map type is not nil, so it is not a zero value, and *new(x) of an
// expression x is the value of x.
func (e *enumerator) isZero(x ast.Expr, dest types.Type) bool {
	info := e.pkg.Info
	x = ast.Unparen(x)
	tv := info.Types[x]
	if tv.IsNil() {
		return true
	}
	if isInterface(dest) && !isInterface(tv.Type) {
		return false
	}
	if v := tv.Value; v != nil {
		switch v.Kind() {
		case constant.Bool:
			return !constant.BoolVal(v)
		case constant.String:
			return constant.StringVal(v) == ""
		default:
			// The constant is a number. Sign returns 0 for zero, and 1 for a
			// constant of unknown value.
			return constant.Sign(v) == 0
		}
	}
	switch n := x.(type) {
	case *ast.Ident:
		v, ok := info.Uses[n].(*types.Var)
		return ok && e.unwritten[v]
	case *ast.StarExpr:
		call, ok := ast.Unparen(n.X).(*ast.CallExpr)
		if !ok {
			return false
		}
		b, ok := e.callee(call).(*types.Builtin)
		return ok && b.Name() == builtinNew && info.Types[call.Args[0]].IsType()
	case *ast.CompositeLit:
		// elem returns the type of the element at index i, whose key is key
		// or nil. The type checker resolves a struct's key to its field.
		var elem func(i int, key ast.Expr) types.Type
		switch u := tv.Type.Underlying().(type) {
		case *types.Struct:
			elem = func(i int, key ast.Expr) types.Type {
				if key != nil {
					return info.Uses[key.(*ast.Ident)].Type()
				}
				return u.Field(i).Type()
			}
		case *types.Array:
			elem = func(int, ast.Expr) types.Type { return u.Elem() }
		default:
			return false
		}
		for i, elt := range n.Elts {
			var key ast.Expr
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				key, elt = kv.Key, kv.Value
			}
			if !e.isZero(elt, elem(i, key)) {
				return false
			}
		}
		return true
	}
	return false
}

// isInterface reports whether t is an interface type and no type parameter,
// whose type set can contain types that are no interface.
func isInterface(t types.Type) bool {
	_, ok := t.Underlying().(*types.Interface)
	return ok && !isTypeParam(t)
}

// unwrittenVariables returns each variable of the package that a variable
// declaration without values declares inside a function's body, and that no
// use writes, so the variable is the zero value of its type wherever a
// return reads it. A use writes the variable when the variable, alone or
// under selectors, indexes, dereferences and parentheses, is the target of
// an assignment, a short variable declaration, an increment, a decrement or
// a range clause, the operand of & or of a slice expression, or the
// receiver of a method with a pointer receiver, as x in x.M() and in the
// method value x.M. Every use in the variable's scope counts, in a function
// literal too and after the return, because a loop can run a later write
// before the return. unwrittenVariables reads every file of the package
// once.
func (e *enumerator) unwrittenVariables() map[*types.Var]bool {
	info := e.pkg.Info
	declared := map[*types.Var]bool{}
	written := map[types.Object]bool{}
	// write notes a write of the variable that x is, alone or under
	// selectors, indexes, dereferences and parentheses.
	write := func(x ast.Expr) {
		for {
			switch n := x.(type) {
			case *ast.ParenExpr:
				x = n.X
			case *ast.SelectorExpr:
				x = n.X
			case *ast.IndexExpr:
				x = n.X
			case *ast.StarExpr:
				x = n.X
			case *ast.Ident:
				written[info.ObjectOf(n)] = true
				return
			default:
				return
			}
		}
	}
	for _, f := range e.pkg.Files {
		ast.Inspect(f.Syntax, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for _, name := range x.Names {
					if v, ok := info.Defs[name].(*types.Var); ok && len(x.Values) == 0 &&
						v.Parent() != e.pkg.Types.Scope() {
						declared[v] = true
					}
				}
			case *ast.AssignStmt:
				for _, target := range x.Lhs {
					write(target)
				}
			case *ast.IncDecStmt:
				write(x.X)
			case *ast.RangeStmt:
				for _, target := range []ast.Expr{x.Key, x.Value} {
					if target != nil {
						write(target)
					}
				}
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					write(x.X)
				}
			case *ast.SliceExpr:
				write(x.X)
			case *ast.SelectorExpr:
				if sel := info.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
					if _, pointer := sel.Obj().Type().(*types.Signature).Recv().Type().(*types.Pointer); pointer {
						write(x.X)
					}
				}
			}
			return true
		})
	}
	for v := range declared {
		if written[v] {
			delete(declared, v)
		}
	}
	return declared
}

// isTerminating reports whether s is a terminating statement in the sense
// of the Go specification, by the rule that go/types applies, for a
// statement that contains no labelled statement. delete keeps every
// statement that contains a label, so a break with a label never decides a
// deletion.
func (e *enumerator) isTerminating(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO || s.Tok == token.FALLTHROUGH
	case *ast.ExprStmt:
		call, ok := ast.Unparen(s.X).(*ast.CallExpr)
		if !ok {
			return false
		}
		b, ok := e.callee(call).(*types.Builtin)
		return ok && b.Name() == builtinPanic
	case *ast.BlockStmt:
		return e.terminates(s.List)
	case *ast.IfStmt:
		return s.Else != nil && e.isTerminating(s.Body) && e.isTerminating(s.Else)
	case *ast.ForStmt:
		return s.Cond == nil && !breaks(s.Body)
	case *ast.SwitchStmt:
		return hasDefault(s.Body) && e.clausesTerminate(s.Body)
	case *ast.TypeSwitchStmt:
		return hasDefault(s.Body) && e.clausesTerminate(s.Body)
	case *ast.SelectStmt:
		return e.clausesTerminate(s.Body)
	}
	return false
}

// terminates reports whether a statement list ends in a terminating
// statement: its final statement that is not empty is one.
func (e *enumerator) terminates(list []ast.Stmt) bool {
	last := final(list)
	return last != nil && e.isTerminating(last)
}

// clausesTerminate reports whether the statement list of every clause of a
// switch, a type switch or a select statement ends in a terminating
// statement and contains no break of the statement.
func (e *enumerator) clausesTerminate(body *ast.BlockStmt) bool {
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			list = c.Body
		case *ast.CommClause:
			list = c.Body
		}
		if !e.terminates(list) || slices.ContainsFunc(list, breaks) {
			return false
		}
	}
	return true
}

// final returns the last statement of list that is not empty, or nil when
// list has none.
func final(list []ast.Stmt) ast.Stmt {
	for _, s := range slices.Backward(list) {
		if _, empty := s.(*ast.EmptyStmt); !empty {
			return s
		}
	}
	return nil
}

// hasDefault reports whether the body of a switch or a type switch
// statement has a default clause.
func hasDefault(body *ast.BlockStmt) bool {
	return slices.ContainsFunc(body.List, func(c ast.Stmt) bool { return c.(*ast.CaseClause).List == nil })
}

// breaks reports whether s is or contains a break statement without a label
// that ends the closest for, switch or select statement around s. A break
// inside a nested for, range, switch, type switch or select statement ends
// that statement.
func breaks(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.BranchStmt:
		return s.Tok == token.BREAK && s.Label == nil
	case *ast.BlockStmt:
		return slices.ContainsFunc(s.List, breaks)
	case *ast.IfStmt:
		return breaks(s.Body) || s.Else != nil && breaks(s.Else)
	}
	return false
}

// hasLabel reports whether s is or contains a labelled statement.
func hasLabel(s ast.Stmt) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		_, ok := n.(*ast.LabeledStmt)
		found = found || ok
		return !found
	})
	return found
}

// allBlank reports whether every expression of es is the blank identifier.
func allBlank(es []ast.Expr) bool {
	for _, x := range es {
		if id, ok := x.(*ast.Ident); !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

func isTypeParam(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.TypeParam)
	return ok
}

// isNumber reports whether t is an integer or floating-point type, or a
// type parameter whose every type is one. A type parameter is one when
// types.Satisfies reports that it satisfies numbers. go/types computes the
// parameter's type set through embedded constraints, unions and
// intersections.
func isNumber(t types.Type) bool {
	if tp, ok := types.Unalias(t).(*types.TypeParam); ok {
		return types.Satisfies(tp, numbers)
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// numbers is the constraint whose type set is every type whose underlying
// type is a typed integer or floating-point type. It is complete, so
// concurrent enumerations only read it.
var numbers = func() *types.Interface {
	var terms []*types.Term
	for _, b := range types.Typ {
		if b.Info()&types.IsUntyped == 0 && b.Info()&(types.IsInteger|types.IsFloat) != 0 {
			terms = append(terms, types.NewTerm(true, b))
		}
	}
	return types.NewInterfaceType(nil, []types.Type{types.NewUnion(terms)}).Complete()
}()

// isPlainBool reports whether t is bool or an untyped boolean.
func isPlainBool(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && (b.Kind() == types.Bool || b.Kind() == types.UntypedBool)
}

// hasContextShift reports whether x contains a non-constant shift whose
// left operand is an untyped constant.
func (e *enumerator) hasContextShift(x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if ok && (b.Op == token.SHL || b.Op == token.SHR) && e.pkg.Info.Types[b].Value == nil && e.untypedConst(b.X) {
			found = true
		}
		return !found
	})
	return found
}

// untypedConst reports whether x is an untyped constant expression. The
// package's type information records the type that such a constant
// converts to, so types.CheckExpr checks x again alone, at its own
// position, where x keeps its untyped type. x type-checks in its package,
// so it type-checks alone, and CheckExpr returns nil.
func (e *enumerator) untypedConst(x ast.Expr) bool {
	if e.pkg.Info.Types[x].Value == nil {
		return false
	}
	alone := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	_ = types.CheckExpr(e.pkg.Fset, e.pkg.Types, x.Pos(), x, alone)
	b, ok := alone.Types[x].Type.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

// sideEffectFree reports whether x is an identifier or a chain of selectors
// on one, whose evaluation has no side effect.
func sideEffectFree(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return sideEffectFree(v.X)
	}
	return false
}
