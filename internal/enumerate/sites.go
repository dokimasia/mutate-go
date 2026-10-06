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
		var parent ast.Node
		inner := fn
		if len(stack) > 0 {
			parent = stack[len(stack)-1].node
			inner = stack[len(stack)-1].fn
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			inner = lit.Type
		}
		if call, ok := n.(*ast.CallExpr); ok {
			e.family(f, call)
		}
		if e.constantSite(n) {
			if constants == 0 {
				e.addSkip(f, n, SkipConstant)
			}
			constants++
		}
		if constants == 0 {
			e.visit(f, n, parent, scope, inner)
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
	}
	return false
}

func (e *enumerator) visit(f *load.File, n, parent ast.Node, scope string, fn *ast.FuncType) {
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
		}
	case *ast.AssignStmt:
		e.compound(f, x, scope)
		e.delete(f, x, parent, scope)
	case *ast.IncDecStmt:
		e.incdec(f, x, parent, scope)
	case *ast.ReturnStmt:
		e.zero(f, x, fn, scope)
	case ast.Stmt:
		e.delete(f, x, parent, scope)
	case *ast.UnaryExpr:
		if x.Op == token.SUB {
			e.minus(f, x, scope)
		}
		e.not(f, x, parent, scope)
	case ast.Expr:
		e.not(f, x, parent, scope)
	}
}

func (e *enumerator) newSite(f *load.File, n ast.Node, scope string, form Form, op token.Token) *Site {
	s := &Site{File: f, Scope: scope, Start: e.off(n.Pos()), End: e.off(n.End()), Form: form, Node: n, Op: op}
	e.sites = append(e.sites, s)
	return s
}

func (e *enumerator) addSkip(f *load.File, n ast.Node, reason string) {
	e.skips = append(e.skips, skip{f: f, start: e.off(n.Pos()), end: e.off(n.End()), reason: reason})
}

// add makes a mutant of kind at s, which writes op in place of the site's
// operator, and whose source is replacement.
func (s *Site) add(kind string, op token.Token, replacement string) *Mutant {
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
// kinds needs only the comparison's result. The walk does not visit a
// comparison whose value is a constant.
func (e *enumerator) equality(f *load.File, n *ast.BinaryExpr, scope string) {
	tv := e.pkg.Info.Types[n]
	if !isPlainBool(tv.Type) {
		e.addSkip(f, n, SkipNamedBool)
		return
	}
	s := e.newSite(f, n, scope, Equality, n.Op)
	s.add(RORTrue, token.ILLEGAL, "true")
	s.add(RORFalse, token.ILLEGAL, "false")
}

// ordered makes the mutants of <, <=, > and >=: the boundary, and false for
// < and > or true for <= and >=. The constant that the catalogue leaves out
// differs from the original wherever the boundary or the kept constant
// does, so they subsume it.
func (e *enumerator) ordered(f *load.File, n *ast.BinaryExpr, scope string) {
	info := e.pkg.Info
	tv := info.Types[n]
	operand := info.TypeOf(n.X)
	switch {
	case !isPlainBool(tv.Type):
		e.addSkip(f, n, SkipNamedBool)
		return
	case isTypeParam(operand):
		e.addSkip(f, n, SkipTypeParameter)
		return
	case e.contextShift(operand, n):
		e.addSkip(f, n, SkipContextShift)
		return
	}
	s := e.newSite(f, n, scope, Ordered, n.Op)
	s.TypeArg = e.typeArg(operand, n.Pos())
	b := boundary[n.Op]
	s.add(RORBoundary, b, e.swapped(f, s, n.OpPos, len(n.Op.String()), b.String()))
	if n.Op == token.LEQ || n.Op == token.GEQ {
		s.add(RORTrue, token.ILLEGAL, "true")
	} else {
		s.add(RORFalse, token.ILLEGAL, "false")
	}
}

// arithmetic makes the mutant of +, -, *, / and % on numbers whose
// operands and result have one type. Arithmetic on numbers whose value is
// a constant is a constant site, which the walk does not visit. A mutant
// that divides an integer by a constant 0 is not viable, with the
// compiler's message as its reason.
func (e *enumerator) arithmetic(f *load.File, n *ast.BinaryExpr, scope string) {
	info := e.pkg.Info
	operand := info.TypeOf(n.X)
	if !isNumber(operand) || !types.Identical(info.TypeOf(n), operand) {
		return
	}
	switch {
	case isTypeParam(operand):
		e.addSkip(f, n, SkipTypeParameter)
		return
	case e.contextShift(operand, n):
		e.addSkip(f, n, SkipContextShift)
		return
	}
	s := e.newSite(f, n, scope, Arithmetic, n.Op)
	s.TypeArg = e.typeArg(operand, n.Pos())
	to := arith[n.Op]
	m := s.add(AOR, to, e.swapped(f, s, n.OpPos, len(n.Op.String()), to.String()))
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
	return hasContextShift(e.pkg.Info, n.X) || hasContextShift(e.pkg.Info, n.Y)
}

// connector makes the mutants of && and ||: each operand alone, and false
// for && or true for ||. A mutant does not evaluate the operands that it
// leaves out. The connector's operands get no negation, because its mutants
// subsume a negated operand.
func (e *enumerator) connector(f *load.File, n *ast.BinaryExpr, scope string) {
	if !isPlainBool(e.pkg.Info.TypeOf(n)) {
		e.addSkip(f, n, SkipNamedBool)
		return
	}
	e.operands[unparen(n.X)] = true
	e.operands[unparen(n.Y)] = true
	s := e.newSite(f, n, scope, Connector, n.Op)
	s.add(LCRLeft, token.ILLEGAL, e.text(f, n.X))
	s.add(LCRRight, token.ILLEGAL, e.text(f, n.Y))
	kind, value := LCRFalse, "false"
	if n.Op == token.LOR {
		kind, value = LCRTrue, "true"
	}
	s.add(kind, token.ILLEGAL, value)
}

// compound makes the mutant of +=, -=, *=, /= and %= on one number. A
// mutant that divides an integer by a constant 0 is not viable, as
// arithmetic states.
func (e *enumerator) compound(f *load.File, n *ast.AssignStmt, scope string) {
	op, ok := assign[n.Tok]
	if !ok || len(n.Lhs) != 1 {
		return
	}
	t := e.pkg.Info.TypeOf(n.Lhs[0])
	if !isNumber(t) {
		return
	}
	switch {
	case isTypeParam(t):
		e.addSkip(f, n, SkipTypeParameter)
		return
	case !sideEffectFree(n.Lhs[0]):
		e.addSkip(f, n, SkipSideEffects)
		return
	}
	s := e.newSite(f, n, scope, Compound, op)
	s.TypeArg = e.typeArg(t, n.Pos())
	to := arith[op]
	m := s.add(AOR, to, e.swapped(f, s, n.TokPos, len(n.Tok.String()), to.String()+"="))
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
			e.addSkip(f, n, SkipTypeParameter)
			return
		case !sideEffectFree(n.X):
			e.addSkip(f, n, SkipSideEffects)
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
	s.add(UOIIncDec, to, e.swapped(f, s, n.TokPos, len(n.Tok.String()), to.String()))
}

// not negates a boolean operand: an identifier, a selector, a call, an
// index, a type assertion, a dereference, or a !x, whose mutant is x. An
// operand of a connector that is a site gets no negation.
func (e *enumerator) not(f *load.File, x ast.Expr, parent ast.Node, scope string) {
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
	if !valuePosition(parent, x) || !ok || tv.Value != nil || !tv.IsValue() || !isPlainBool(tv.Type) {
		return
	}
	s := e.newSite(f, x, scope, Not, token.ILLEGAL)
	replacement := "!" + e.text(f, x)
	if operand != nil {
		replacement = e.text(f, operand)
	}
	s.add(UOINot, token.ILLEGAL, replacement)
}

// valuePosition reports whether x, a child of parent, is in a position
// where a negation may replace it: not the target of an assignment, not
// the operand of & or !, not a composite literal's key, not a selector's
// name, not a called function, and not the expression of an increment, an
// expression statement, a deferred call, a go statement or a range clause.
func valuePosition(parent ast.Node, x ast.Expr) bool {
	switch p := parent.(type) {
	case *ast.AssignStmt:
		return !slices.Contains(p.Lhs, x)
	case *ast.UnaryExpr:
		return p.Op != token.AND && p.Op != token.NOT
	case *ast.KeyValueExpr:
		return p.Key != x
	case *ast.SelectorExpr:
		return ast.Expr(p.Sel) != x
	case *ast.CallExpr:
		return p.Fun != x
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
		e.addSkip(f, n, SkipTypeParameter)
		return
	}
	s := e.newSite(f, n, scope, Minus, token.SUB)
	s.TypeArg = e.typeArg(tv.Type, n.Pos())
	s.add(UOIMinus, token.ILLEGAL, e.text(f, n.X))
}

// delete removes one statement of a statement list. It keeps
// declarations, labels, branches, returns, increments and decrements, and a
// last statement that terminates its list.
func (e *enumerator) delete(f *load.File, st ast.Stmt, parent ast.Node, scope string) {
	var list []ast.Stmt
	switch p := parent.(type) {
	case *ast.BlockStmt:
		list = p.List
	case *ast.CaseClause:
		list = p.Body
	case *ast.CommClause:
		list = p.Body
	default:
		return
	}
	if !slices.Contains(list, st) {
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
	if list[len(list)-1] == st && isTerminating(e.pkg.Info, st) || hasLabel(st) {
		return
	}
	s := e.newSite(f, st, scope, Delete, token.ILLEGAL)
	s.add(SBRDelete, token.ILLEGAL, "")
}

// zero makes the mutant that returns the zero value of every result type
// before a return statement whose results are not all zero values already.
// The mutant does not evaluate the results.
func (e *enumerator) zero(f *load.File, n *ast.ReturnStmt, fn *ast.FuncType, scope string) {
	if fn == nil || fn.Results == nil || len(n.Results) == 0 || allZero(e.pkg.Info, n.Results) {
		return
	}
	var zeros []string
	for _, field := range fn.Results.List {
		zero := e.zeroOf(f, field.Type)
		for i := 0; i < max(len(field.Names), 1); i++ {
			zeros = append(zeros, zero)
		}
	}
	s := e.newSite(f, n, scope, Zero, token.ILLEGAL)
	s.Zeros = zeros
	s.add(SBRZero, token.ILLEGAL, "return "+strings.Join(zeros, ", "))
}

// zeroOf returns the zero value of the type that the expression t writes,
// as Go code writes it: 0, "", false or nil by the underlying type, the
// type followed by {} for a struct or an array, and *new(T) for a type
// parameter T, which has no literal.
func (e *enumerator) zeroOf(f *load.File, t ast.Expr) string {
	typ := e.pkg.Info.TypeOf(t)
	text := e.text(f, unparen(t))
	if isTypeParam(typ) {
		return "*new(" + text + ")"
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

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// allZero reports whether every result is the zero value of its type.
func allZero(info *types.Info, results []ast.Expr) bool {
	for _, e := range results {
		if !isZero(info, e) {
			return false
		}
	}
	return true
}

// isZero reports whether x is the zero value of its type: nil, a constant
// whose value is 0, "" or false, *new(T) of a type T, or a composite literal
// of a struct or an array type whose every element is such a value. A
// composite literal of a slice or a map type is not nil, so it is not a
// zero value, and *new(x) of an expression x is the value of x.
func isZero(info *types.Info, x ast.Expr) bool {
	x = unparen(x)
	tv := info.Types[x]
	if tv.IsNil() {
		return true
	}
	if v := tv.Value; v != nil {
		switch v.Kind() {
		case constant.Bool:
			return !constant.BoolVal(v)
		case constant.String:
			return constant.StringVal(v) == ""
		}
		// The constant is a number. Sign returns 0 for zero, and 1 for a
		// constant of unknown value.
		return constant.Sign(v) == 0
	}
	switch n := x.(type) {
	case *ast.StarExpr:
		call, ok := unparen(n.X).(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := unparen(call.Fun).(*ast.Ident)
		if !ok {
			return false
		}
		_, builtin := info.Uses[id].(*types.Builtin)
		return builtin && id.Name == "new" && info.Types[call.Args[0]].IsType()
	case *ast.CompositeLit:
		switch tv.Type.Underlying().(type) {
		case *types.Struct, *types.Array:
		default:
			return false
		}
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			if !isZero(info, elt) {
				return false
			}
		}
		return true
	}
	return false
}

// isTerminating reports whether s is a terminating statement in the sense
// of the Go specification. It errs towards yes, which only keeps a
// statement from being deleted.
func isTerminating(info *types.Info, s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := unparen(call.Fun).(*ast.Ident)
		if !ok {
			return false
		}
		_, builtin := info.Uses[id].(*types.Builtin)
		return builtin && id.Name == "panic"
	case *ast.BlockStmt:
		return len(s.List) > 0 && isTerminating(info, s.List[len(s.List)-1])
	case *ast.IfStmt:
		return s.Else != nil && isTerminating(info, s.Body) && isTerminating(info, s.Else)
	case *ast.ForStmt:
		return s.Cond == nil
	case *ast.SwitchStmt:
		return clausesTerminate(info, s.Body)
	case *ast.TypeSwitchStmt:
		return clausesTerminate(info, s.Body)
	case *ast.SelectStmt:
		return clausesTerminate(info, s.Body)
	case *ast.LabeledStmt:
		return isTerminating(info, s.Stmt)
	}
	return false
}

func clausesTerminate(info *types.Info, body *ast.BlockStmt) bool {
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			list = c.Body
		case *ast.CommClause:
			list = c.Body
		}
		if len(list) == 0 {
			return false
		}
		last := list[len(list)-1]
		if b, ok := last.(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
			continue
		}
		if !isTerminating(info, last) {
			return false
		}
	}
	return true
}

func hasLabel(s ast.Stmt) bool {
	found := false
	ast.Inspect(s, func(n ast.Node) bool {
		_, ok := n.(*ast.LabeledStmt)
		found = found || ok
		return !found
	})
	return found
}

func allBlank(es []ast.Expr) bool {
	for _, e := range es {
		if id, ok := e.(*ast.Ident); !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

func isTypeParam(t types.Type) bool {
	_, ok := unalias(t).(*types.TypeParam)
	return ok
}

// isNumber reports whether t is an integer or floating-point type, or a
// type parameter whose constraint embeds only unions of such types. The
// walk passes isNumber a type parameter only as the type of an operand of
// arithmetic, so its constraint embeds at least one type.
func isNumber(t types.Type) bool {
	if tp, ok := unalias(t).(*types.TypeParam); ok {
		iface := tp.Underlying().(*types.Interface)
		for i := 0; i < iface.NumEmbeddeds(); i++ {
			u, ok := iface.EmbeddedType(i).(*types.Union)
			if !ok {
				return false
			}
			for j := 0; j < u.Len(); j++ {
				if !isNumber(u.Term(j).Type()) {
					return false
				}
			}
		}
		return true
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// isPlainBool reports whether t is bool or an untyped boolean.
func isPlainBool(t types.Type) bool {
	b, ok := unalias(t).(*types.Basic)
	return ok && (b.Kind() == types.Bool || b.Kind() == types.UntypedBool)
}

func isUntyped(t types.Type) bool {
	b, ok := unalias(t).(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

// hasContextShift reports whether x contains a non-constant shift whose
// left operand is an untyped constant.
func hasContextShift(info *types.Info, x ast.Expr) bool {
	found := false
	ast.Inspect(x, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if ok && (b.Op == token.SHL || b.Op == token.SHR) && info.Types[b].Value == nil && untypedConst(info, b.X) {
			found = true
		}
		return !found
	})
	return found
}

// untypedConst reports whether x is an untyped constant expression. The
// type checker records the type that such a constant converts to, so the
// test reads the syntax: literals, untyped named constants, and operators
// on them.
func untypedConst(info *types.Info, x ast.Expr) bool {
	switch v := unparen(x).(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		c, ok := info.Uses[v].(*types.Const)
		return ok && isUntyped(c.Type())
	case *ast.SelectorExpr:
		c, ok := info.Uses[v.Sel].(*types.Const)
		return ok && isUntyped(c.Type())
	case *ast.UnaryExpr:
		return untypedConst(info, v.X)
	case *ast.BinaryExpr:
		return untypedConst(info, v.X) && untypedConst(info, v.Y)
	}
	return false
}

func sideEffectFree(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return sideEffectFree(v.X)
	}
	return false
}
