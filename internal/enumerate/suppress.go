// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// annotation is one annotation comment and the line that it covers.
type annotation struct {
	f      *load.File
	line   int
	kinds  map[string]bool
	reason string
	// where is the comment's file and line, as a message states them.
	where string
	used  bool
}

// suppression is the code that a rule family suppresses. For a call of a
// call family, call is the call: the call itself, a statement that makes
// it, and every site inside its parentheses are suppressed. For capacity,
// call is nil and the range is the argument. For a compound statement that
// a call family suppresses as a whole, call is nil and the range is the
// statement.
type suppression struct {
	f          *load.File
	start, end int
	family     string
	call       *ast.CallExpr
}

// callee returns the object that call calls, as the type checker resolves
// the call's function without its parentheses and its type arguments: a
// function, a method, a builtin or a variable. It returns nil for a function
// that is no name, such as a function literal.
func (e *enumerator) callee(call *ast.CallExpr) types.Object {
	fun := unparen(call.Fun)
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
		e.suppressions = append(
			e.suppressions,
			suppression{f: f, start: e.off(call.Pos()), end: e.off(call.End()), family: family, call: call},
		)
		return
	}
	for _, c := range e.def.Overlay.Families.Capacity {
		if c.Func != name || c.Argument >= len(call.Args) {
			continue
		}
		if name == "make" && allocated(info.TypeOf(call.Args[0])) != c.Of {
			continue
		}
		arg := call.Args[c.Argument]
		e.suppressions = append(
			e.suppressions,
			suppression{f: f, start: e.off(arg.Pos()), end: e.off(arg.End()), family: "capacity"},
		)
	}
}

// method returns the family of the overlay's method rule that fn matches:
// a method of an interface with the rule's name and signature. The
// signature is the method's type as go/types writes it, without the
// receiver and without parameter names.
func (e *enumerator) method(fn *types.Func) (string, bool) {
	sig := fn.Type().(*types.Signature)
	if sig.Recv() == nil {
		return "", false
	}
	if _, onInterface := sig.Recv().Type().Underlying().(*types.Interface); !onInterface {
		return "", false
	}
	bare := types.NewSignatureType(nil, nil, nil, unnamed(sig.Params()), unnamed(sig.Results()), sig.Variadic())
	for _, m := range e.def.Overlay.Families.Methods {
		if m.On == "interface" && m.Name == fn.Name() && m.Signature == bare.String() {
			return m.Family, true
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
func (e *enumerator) resultVariables() map[*types.Var]string {
	info := e.pkg.Info
	rules := map[spec.Result]bool{}
	for _, r := range e.def.Overlay.Families.Results {
		rules[r] = true
	}
	// result returns the family of a rule that the result at index k of
	// call matches, or "". The package type-checks, so the called function
	// of a listed API has a signature with a result at index k.
	result := func(call *ast.CallExpr, k int) string {
		family := ""
		if fn, isFunc := e.callee(call).(*types.Func); isFunc {
			family = e.families[fn.FullName()]
		}
		if family == "" {
			return ""
		}
		results := info.TypeOf(call.Fun).(*types.Signature).Results()
		if rules[spec.Result{Family: family, Type: types.TypeString(results.At(k).Type(), nil)}] {
			return family
		}
		return ""
	}
	families := map[*types.Var]string{}
	other := map[*types.Var]bool{}
	// assign notes that x, when it is a variable, gets a value: a result of
	// family, or any other value when family is "".
	assign := func(x ast.Expr, family string) {
		id, ok := unparen(x).(*ast.Ident)
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
			family := ""
			if call, ok := unparen(value).(*ast.CallExpr); ok {
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
		if f.Role != load.Source && f.Role != load.Cgo {
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

// allocated names the kind of collection that make allocates for t.
func allocated(t types.Type) string {
	switch t.Underlying().(type) {
	case *types.Slice:
		return "slice"
	case *types.Map:
		return "map"
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
			list, reason, ok := strings.Cut(rest, ":")
			reason = strings.TrimSpace(reason)
			if !ok || reason == "" {
				e.problems = append(
					e.problems,
					Problem{Code: ErrorWithoutReason, Message: where + ": the annotation states no reason"},
				)
				continue
			}
			kinds := map[string]bool{}
			for _, k := range strings.Split(list, ",") {
				kinds[strings.TrimSpace(k)] = true
			}
			e.annotations = append(
				e.annotations,
				&annotation{f: f, line: line, kinds: kinds, reason: reason, where: where},
			)
		}
	}
}

// suppress applies the rule families, then the annotations, to m.
func (e *enumerator) suppress(m *Mutant) {
	s := m.Site
	for _, sup := range e.suppressions {
		if sup.f != s.File {
			continue
		}
		inside := sup.start <= s.Start && s.End <= sup.end
		if sup.call != nil {
			args := e.off(sup.call.Lparen) < s.Start && s.End <= e.off(sup.call.Rparen)
			whole := s.Start == sup.start && s.End == sup.end
			inside = args || whole || s.Form == Delete && callOf(s.Node.(ast.Stmt)) == sup.call
		}
		if inside {
			m.Status, m.Rule, m.Reason = Suppressed, sup.family, ""
			return
		}
	}
	for _, a := range e.annotations {
		if a.f == s.File && a.line == s.StartPos.Line &&
			(a.kinds[m.Kind] || a.kinds[e.classes[m.Kind]] || a.kinds["all"]) {
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
		call, _ := unparen(s.X).(*ast.CallExpr)
		return call
	case *ast.DeferStmt:
		return s.Call
	case *ast.GoStmt:
		return s.Call
	}
	return nil
}
