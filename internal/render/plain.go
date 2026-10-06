// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"go/ast"
	"go/token"
	"strings"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// Plain returns the program whose one file is the file of m's site, with m
// written into it without a switch. The build of that program is m's
// ordinary build.
//
// The change evaluates the operands as m's expression does. It keeps the
// code that m leaves out behind a constant that skips it, so every name that
// the file uses remains in use. It writes the constants true and false as
// (0 == 0) and (0 != 0), which no declaration of the program can hide:
//
//   - aor, ror-boundary and uoi-incdec write their operator in place of the
//     site's.
//   - ror-true and ror-false keep the comparison and join the constant to
//     it, as (a < b || (0 == 0)) and (a < b && (0 != 0)), so both operands
//     are still evaluated.
//   - lcr-left writes ((a) || (0 != 0) && (b)) for a && b or a || b,
//     lcr-right ((0 != 0) && (a) || (b)), lcr-true ((0 == 0) || (a) || (b))
//     and lcr-false ((0 != 0) && ((a) && (b))).
//   - uoi-not writes !(x) for x, and (x) for !x, and uoi-minus writes (x) for
//     -x.
//   - sbr-delete writes if (0 != 0) { s } for the statement s, and sbr-zero
//     writes if (0 == 0) { return z }; before the return statement, with z
//     the variables that zeroBindings binds to the function's results.
//
// The change keeps every line of the file at its number: it writes the
// line breaks of the source that it removes where a line break ends no
// statement, after an opening parenthesis or after a connector.
//
// # Allocation contract
//
// Plain allocates the copy of the file, the change's text and, for sbr-zero,
// the bindings of the results.
func Plain(p *load.Package, m *enumerate.Mutant) *Program {
	s := m.Site
	text := s.File.Text
	off := func(pos token.Pos) int { return p.Fset.File(pos).Offset(pos) }
	source := func(n ast.Node) string { return string(text[off(n.Pos()):off(n.End())]) }
	start, end := s.Start, s.End
	// The change is head, the line breaks that it removes, and body. edits
	// are the edits before it.
	var head, body string
	var edits []edit
	switch m.Kind {
	case spec.AOR, spec.RORBoundary, spec.UOIIncDec:
		var at token.Pos
		var op string
		head = m.Op.String()
		switch n := s.Node.(type) {
		case *ast.BinaryExpr:
			at, op = n.OpPos, n.Op.String()
		case *ast.AssignStmt:
			at, op, head = n.TokPos, n.Tok.String(), head+"="
		case *ast.IncDecStmt:
			at, op = n.TokPos, n.Tok.String()
		}
		start = off(at)
		end = start + len(op)
	case spec.RORTrue:
		head = "(" + source(s.Node) + " || " + trueExpr + ")"
	case spec.RORFalse:
		head = "(" + source(s.Node) + " && " + falseExpr + ")"
	case spec.LCRLeft, spec.LCRRight, spec.LCRTrue, spec.LCRFalse:
		n := s.Node.(*ast.BinaryExpr)
		x, y := "("+source(n.X)+")", "("+source(n.Y)+")"
		switch m.Kind {
		case spec.LCRLeft:
			head, body = "("+x+" || "+falseExpr+" && ", y+")"
		case spec.LCRRight:
			head, body = "("+falseExpr+" && "+x+" || ", y+")"
		case spec.LCRTrue:
			head, body = "("+trueExpr+" || "+x+" || ", y+")"
		default:
			head, body = "("+falseExpr+" && ("+x+" && ", y+"))"
		}
	case spec.UOINot:
		head = "!(" + source(s.Node) + ")"
		if n, ok := s.Node.(*ast.UnaryExpr); ok && n.Op == token.NOT {
			head, body = "(", source(n.X)+")"
		}
	case spec.UOIMinus:
		head, body = "(", source(s.Node.(*ast.UnaryExpr).X)+")"
	case spec.SBRDelete:
		head = "if " + falseExpr + " { " + source(s.Node) + " }"
	case spec.SBRZero:
		var vars []string
		edits, vars = zeroBindings(p.Fset, s.Func)
		head = "if " + trueExpr + " { return " + strings.Join(vars, ", ") + " }; " + source(s.Node)
	}
	breaks := bytes.Count(text[start:end], []byte("\n")) - strings.Count(head+body, "\n")
	change := head + strings.Repeat("\n", max(0, breaks)) + body
	r := &fileRenderer{text: text, edits: append(edits, edit{start: start, end: end, text: change})}
	r.buf.Grow(len(text) + len(change))
	r.writeText(0, len(text))
	return &Program{Files: map[string][]byte{s.File.Path: r.buf.Bytes()}}
}
