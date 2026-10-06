// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"math"
	"slices"
	"sort"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// The text of an annotation: the separator of its kinds from its reason,
// and of one kind from the next.
const (
	reasonSeparator = ":"
	kindSeparator   = ","
)

// fileLine is one line of a file, the key of the annotations that cover it.
type fileLine struct {
	f    *load.File
	line int
}

// annotation is one annotation comment. The enumerator keys it by the line
// that it covers.
type annotation struct {
	// kinds contains each name that the annotation lists, and unknown the
	// names that are no kind, class or keyword of the catalogue.
	kinds   map[string]bool
	unknown []string
	reason  string
	// where is the comment's file and line, as a message states them.
	where string
	used  bool
}

// stale returns the message of an annotation that suppresses no mutant,
// with the names that it lists and the catalogue does not define.
func (a *annotation) stale() string {
	message := a.where + ": the annotation suppresses no mutant"
	if len(a.unknown) > 0 {
		message += ", and the catalogue does not define " + strings.Join(a.unknown, " or ")
	}
	return message
}

// suppression is the code that a rule family suppresses in one file. For a
// call of a family that lists calls, call is the call, and lparen and
// rparen are the offsets of its parentheses: the call itself, a statement
// that makes it, and every site inside its parentheses are suppressed. For
// an argument rule, call is nil and the range is the argument. For a
// compound statement that a family suppresses as a whole, or a part of
// one, call is nil and the range is the statement or the part.
type suppression struct {
	start, end     int
	lparen, rparen int
	family         spec.Family
	call           *ast.CallExpr
}

// resultRule is a result rule of a family: the family, and the type of the
// result that the rule names.
type resultRule struct {
	family spec.Family
	typ    string
}

// callee returns the object that call calls, as the type checker resolves
// the call's function without its parentheses and its type arguments: a
// function, a method, a builtin or a variable. It returns nil for a function
// that is no name, such as a function literal.
func (e *enumerator) callee(call *ast.CallExpr) types.Object {
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		fun = x.X
	case *ast.IndexListExpr:
		fun = x.X
	}
	switch x := fun.(type) {
	case *ast.Ident:
		return e.pkg.Info.Uses[x]
	case *ast.SelectorExpr:
		return e.pkg.Info.Uses[x.Sel]
	}
	return nil
}

// apiName returns the name of obj as the overlay spells an API: a function's
// or a method's full name, or a builtin's name. It returns "" for any other
// object, such as a variable.
func apiName(obj types.Object) string {
	switch o := obj.(type) {
	case *types.Func:
		return o.FullName()
	case *types.Builtin:
		return o.Name()
	}
	return ""
}

// family records what a call to an API of a rule family suppresses. The
// type checker resolves the API: a function or a method by its full name or
// by a method rule of the overlay, a variable by a result rule, and the
// builtin make by its name and the allocated type. Only a local variable
// that an identifier names can match a result rule.
func (e *enumerator) family(f *load.File, call *ast.CallExpr) {
	info := e.pkg.Info
	obj := e.callee(call)
	name := apiName(obj)
	family, ok := e.families[name]
	if fn, isFunc := obj.(*types.Func); !ok && isFunc {
		family, ok = e.method(fn)
	}
	if v, isVar := obj.(*types.Var); !ok && isVar {
		family, ok = e.results[v]
	}
	if ok {
		e.calls[call] = family
		e.suppressions[f] = append(e.suppressions[f], suppression{
			start: e.off(call.Pos()), end: e.off(call.End()), lparen: e.off(call.Lparen), rparen: e.off(call.Rparen),
			family: family, call: call,
		})
		return
	}
	// A rule names an argument by the index of the API's parameter. A call of
	// a method expression passes the receiver first, so the argument is one
	// place later there.
	receiver := 0
	if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel {
		if s := info.Selections[sel]; s != nil && s.Kind() == types.MethodExpr {
			receiver = 1
		}
	}
	// The overlay states each argument rule in one family, so one family at
	// most suppresses an argument, whatever the order of the map.
	for family, rules := range e.def.Overlay.Families {
		for _, a := range rules.Arguments {
			at := a.Argument + receiver
			if a.Func != name || at >= len(call.Args) {
				continue
			}
			if name == spec.Make && allocated(info.TypeOf(call.Args[0])) != a.Of {
				continue
			}
			arg := call.Args[at]
			e.suppressions[f] = append(
				e.suppressions[f],
				suppression{start: e.off(arg.Pos()), end: e.off(arg.End()), family: family},
			)
		}
	}
}

// method returns the family of the overlay's method rule that fn matches:
// a method of an interface with the rule's name and signature. The
// signature is the method's type as go/types writes it, without the
// receiver and without parameter names. The overlay states each method rule
// in one family.
func (e *enumerator) method(fn *types.Func) (spec.Family, bool) {
	sig := fn.Type().(*types.Signature)
	if sig.Recv() == nil {
		return "", false
	}
	if _, onInterface := sig.Recv().Type().Underlying().(*types.Interface); !onInterface {
		return "", false
	}
	bare := types.NewSignatureType(nil, nil, nil, unnamed(sig.Params()), unnamed(sig.Results()), sig.Variadic())
	for family, rules := range e.def.Overlay.Families {
		for _, m := range rules.Methods {
			if m.On == spec.OnInterface && m.Name == fn.Name() && m.Signature == bare.String() {
				return family, true
			}
		}
	}
	return "", false
}

// resultVariables returns, by variable, the family of each variable of the
// files that the enumeration walks whose calls a result rule of the overlay
// puts into the family: a variable declared inside a function's body that an
// assignment gives the result of the rule's type of a call of the family's
// API, that every other assignment gives such a result of the same family,
// and whose address no expression takes. A declaration without values
// assigns nothing.
func (e *enumerator) resultVariables() map[*types.Var]spec.Family {
	info := e.pkg.Info
	rules := map[resultRule]bool{}
	for family, r := range e.def.Overlay.Families {
		for _, result := range r.Results {
			rules[resultRule{family: family, typ: result.Type}] = true
		}
	}
	// result returns the family of a rule that the result at index k of
	// call matches, or "". The package type-checks, so the called function
	// of a listed API has a signature with a result at index k.
	result := func(call *ast.CallExpr, k int) spec.Family {
		var family spec.Family
		if fn, isFunc := e.callee(call).(*types.Func); isFunc {
			family = e.families[fn.FullName()]
		}
		if family == "" {
			return ""
		}
		results := info.TypeOf(call.Fun).(*types.Signature).Results()
		if rules[resultRule{family: family, typ: types.TypeString(results.At(k).Type(), nil)}] {
			return family
		}
		return ""
	}
	families := map[*types.Var]spec.Family{}
	other := map[*types.Var]bool{}
	// assign notes that x, when it is a variable, gets a value: a result of
	// family, or any other value when family is "".
	assign := func(x ast.Expr, family spec.Family) {
		id, ok := ast.Unparen(x).(*ast.Ident)
		if !ok {
			return
		}
		v, ok := info.ObjectOf(id).(*types.Var)
		switch {
		case !ok:
		case family == "" || families[v] != "" && families[v] != family:
			other[v] = true
		default:
			families[v] = family
		}
	}
	// values notes the assignment of values to targets: the results of one
	// call, or one value per target.
	values := func(targets, values []ast.Expr) {
		for k, x := range targets {
			value, at := values[0], k
			if len(values) == len(targets) {
				value, at = values[k], 0
			}
			var family spec.Family
			if call, ok := ast.Unparen(value).(*ast.CallExpr); ok {
				family = result(call, at)
			}
			assign(x, family)
		}
	}
	params := map[types.Object]bool{}
	declare := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				params[info.Defs[name]] = true
			}
		}
	}
	for _, f := range e.pkg.Files {
		if !e.target(f) {
			continue
		}
		ast.Inspect(f.Syntax, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				declare(x.Recv)
			case *ast.FuncType:
				declare(x.Params)
				declare(x.Results)
			case *ast.AssignStmt:
				if x.Tok == token.ASSIGN || x.Tok == token.DEFINE {
					values(x.Lhs, x.Rhs)
				} else {
					assign(x.Lhs[0], "")
				}
			case *ast.ValueSpec:
				if len(x.Values) > 0 {
					names := make([]ast.Expr, len(x.Names))
					for i, name := range x.Names {
						names[i] = name
					}
					values(names, x.Values)
				}
			case *ast.RangeStmt:
				for _, target := range []ast.Expr{x.Key, x.Value} {
					if target != nil {
						assign(target, "")
					}
				}
			case *ast.UnaryExpr:
				if x.Op == token.AND {
					assign(x.X, "")
				}
			}
			return true
		})
	}
	for v := range families {
		if other[v] || params[v] || v.Parent() == e.pkg.Types.Scope() {
			delete(families, v)
		}
	}
	return families
}

// unnamed returns the types of t's variables without their names.
func unnamed(t *types.Tuple) *types.Tuple {
	vars := make([]*types.Var, t.Len())
	for i := range vars {
		vars[i] = types.NewParam(token.NoPos, nil, "", t.At(i).Type())
	}
	return types.NewTuple(vars...)
}

// allocated names the kind of collection that make allocates for t, as an
// argument rule names it, or "" for a channel, which no rule names.
func allocated(t types.Type) string {
	switch t.Underlying().(type) {
	case *types.Slice:
		return spec.OfSlice
	case *types.Map:
		return spec.OfMap
	}
	return ""
}

// parseAnnotations reads the annotation comments of f. A comment on a line
// of its own covers the next line, and one after code covers its own line.
func (e *enumerator) parseAnnotations(f *load.File) {
	directive := e.def.Overlay.Comment + e.def.Catalogue.Annotation
	tf := e.pkg.Fset.File(f.Syntax.Package)
	for _, group := range f.Syntax.Comments {
		for _, c := range group.List {
			rest, ok := strings.CutPrefix(c.Text, directive)
			if !ok || rest != "" && rest[0] != ' ' {
				continue
			}
			raw := tf.PositionFor(c.Pos(), false)
			pos := e.position(f, raw.Offset)
			where := fmt.Sprintf("%s:%d", f.Name, pos.Line)
			line := pos.Line
			if strings.TrimSpace(string(f.Text[raw.Offset-(raw.Column-1):raw.Offset])) == "" {
				line++
			}
			list, reason, ok := strings.Cut(rest, reasonSeparator)
			reason = strings.TrimSpace(reason)
			if !ok || reason == "" {
				e.problems = append(
					e.problems,
					Problem{Code: spec.ErrorWithoutReason, Message: where + ": the annotation states no reason"},
				)
				continue
			}
			a := &annotation{kinds: map[string]bool{}, reason: reason, where: where}
			for k := range strings.SplitSeq(list, kindSeparator) {
				name := strings.TrimSpace(k)
				a.kinds[name] = true
				if !e.names[name] {
					a.unknown = append(a.unknown, name)
				}
			}
			e.annotations = append(e.annotations, a)
			covered := fileLine{f: f, line: line}
			e.covers[covered] = append(e.covers[covered], a)
		}
	}
}

// ruleIndex finds, for a site of one file, the first suppression of the
// file, in the order that the enumeration makes them, that suppresses the
// site: a range that contains the site, the parentheses of a call that
// contain the site, the call itself, or the call that the site's statement
// makes.
//
// The ranges and the parentheses are ranges of the syntax tree, so any two
// are disjoint or nested, and the ones that contain a site form a chain.
// nodes lists them by start, the longer of two with one start first, each
// with the innermost node that contains it. A site's innermost node is the
// last node that starts at or before the site, or the first ancestor of that
// node that ends at or after the site's end, and the node's first is the
// node of least order in its chain. A lookup costs a binary search and the
// walk up from that node, which is at most the depth of the ranges'
// nesting.
//
// # Allocation contract
//
// newRuleIndex allocates the nodes and two maps. rule does not allocate.
type ruleIndex struct {
	nodes []ruleNode
	// whole maps the range of each call that a family suppresses, and calls
	// the call, to the first of its suppressions.
	whole map[[2]int]ruleNode
	calls map[*ast.CallExpr]ruleNode
}

// ruleNode is one suppression of a ruleIndex: the range of the sites that
// it contains, from start to end, its family, and its position in the
// enumeration's order. parent is the index of the innermost node that
// contains the node, or -1, and first is the index of the node of least
// order among the node and its ancestors.
type ruleNode struct {
	start, end    int
	family        spec.Family
	order         int
	parent, first int
}

// newRuleIndex indexes sups, the suppressions of one file in the order that
// the enumeration makes them.
func newRuleIndex(sups []suppression) *ruleIndex {
	x := &ruleIndex{whole: map[[2]int]ruleNode{}, calls: map[*ast.CallExpr]ruleNode{}}
	for order, sup := range sups {
		n := ruleNode{start: sup.start, end: sup.end, family: sup.family, order: order}
		if sup.call != nil {
			whole := [2]int{sup.start, sup.end}
			if _, ok := x.whole[whole]; !ok {
				x.whole[whole] = n
			}
			if _, ok := x.calls[sup.call]; !ok {
				x.calls[sup.call] = n
			}
			n.start, n.end = sup.lparen+1, sup.rparen
		}
		x.nodes = append(x.nodes, n)
	}
	slices.SortStableFunc(x.nodes, func(a, b ruleNode) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})
	var stack []int
	for i := range x.nodes {
		n := &x.nodes[i]
		for len(stack) > 0 && x.nodes[stack[len(stack)-1]].end < n.end {
			stack = stack[:len(stack)-1]
		}
		n.parent, n.first = -1, i
		if len(stack) > 0 {
			n.parent = stack[len(stack)-1]
			if p := x.nodes[n.parent].first; x.nodes[p].order < n.order {
				n.first = p
			}
		}
		stack = append(stack, i)
	}
	return x
}

// rule returns the family of the first suppression that suppresses s, or
// "" when none does.
func (x *ruleIndex) rule(s *Site) spec.Family {
	best := ruleNode{order: math.MaxInt}
	i := sort.Search(len(x.nodes), func(i int) bool { return x.nodes[i].start > s.Start }) - 1
	for i >= 0 && x.nodes[i].end < s.End {
		i = x.nodes[i].parent
	}
	if i >= 0 {
		best = x.nodes[x.nodes[i].first]
	}
	if n, ok := x.whole[[2]int{s.Start, s.End}]; ok && n.order < best.order {
		best = n
	}
	if s.Form == Delete {
		if n, ok := x.calls[callOf(s.Node.(ast.Stmt))]; ok && n.order < best.order {
			best = n
		}
	}
	return best.family
}

// suppress suppresses m by rule, the family that a ruleIndex returns for
// m's site, or else by the first annotation of the site's line that lists
// m's kind, its class or the keyword of every kind.
func (e *enumerator) suppress(m *Mutant, rule spec.Family) {
	if rule != "" {
		m.Status, m.Rule, m.Reason = Suppressed, rule, ""
		return
	}
	for _, a := range e.covers[fileLine{f: m.Site.File, line: m.Site.StartPos.Line}] {
		if a.kinds[string(m.Kind)] || a.kinds[string(e.classes[m.Kind])] || a.kinds[e.def.Catalogue.Every] {
			m.Status, m.Reason = Suppressed, a.reason
			a.used = true
			return
		}
	}
}

// callOf returns the call that an expression, defer or go statement
// makes.
func callOf(st ast.Stmt) *ast.CallExpr {
	switch s := st.(type) {
	case *ast.ExprStmt:
		call, _ := ast.Unparen(s.X).(*ast.CallExpr)
		return call
	case *ast.DeferStmt:
		return s.Call
	case *ast.GoStmt:
		return s.Call
	}
	return nil
}
