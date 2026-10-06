// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/scanner"
	"go/token"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// span is the range of one site's form in an instrumented text.
type span struct {
	start, end int
	site       *enumerate.Site
}

// fileRenderer writes one instrumented file. edits lists the edits of the
// text outside the forms in the order of their offsets, and next is the
// first edit that the renderer has not written. zeros maps the function of
// each Zero site to the variables that the site's form returns, separated
// by commas.
type fileRenderer struct {
	fset     *token.FileSet
	text     []byte
	first    map[*enumerate.Site]int
	children map[*enumerate.Site][]*enumerate.Site
	edits    []edit
	next     int
	zeros    map[*enumerate.Function]string
	buf      bytes.Buffer
	spans    []span
}

// renderFile returns the text of f with each of sites replaced by its
// form, and the range of each form in that text. first maps each site to
// its first ordinal. The text binds the results of each function that a
// Zero site of sites returns from, as zeroBindings states. When lift is
// true, the text starts with a build constraint that raises the file's
// language version to at least go1.18, and a line directive that keeps
// every line's number.
func renderFile(
	fset *token.FileSet,
	f *load.File,
	sites []*enumerate.Site,
	first map[*enumerate.Site]int,
	lift bool,
) ([]byte, []span) {
	r := &fileRenderer{
		fset:     fset,
		text:     f.Text,
		first:    first,
		children: map[*enumerate.Site][]*enumerate.Site{},
		zeros:    map[*enumerate.Function]string{},
	}
	for _, s := range sites {
		if s.Form != enumerate.Zero || r.zeros[s.Func] != "" {
			continue
		}
		edits, vars := zeroBindings(fset, s.Func)
		r.edits, r.zeros[s.Func] = append(r.edits, edits...), strings.Join(vars, ", ")
	}
	slices.SortStableFunc(r.edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	prefix := ""
	if lift {
		prefix, r.text = liftVersion(fset, f)
	}
	r.buf.WriteString(prefix)
	r.writeRange(0, len(r.text), nest(sites, r.children))
	return r.buf.Bytes(), r.spans
}

// nest sorts sites so that each site follows the sites that contain it,
// records each site's directly nested sites in children, and returns the
// outermost sites. Two sites with one range are a compound assignment and
// its deletion, and the deletion contains the assignment.
func nest(sites []*enumerate.Site, children map[*enumerate.Site][]*enumerate.Site) []*enumerate.Site {
	sorted := append([]*enumerate.Site(nil), sites...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End > b.End
		}
		return a.Form == enumerate.Delete && b.Form != enumerate.Delete
	})
	var roots, stack []*enumerate.Site
	for _, s := range sorted {
		for len(stack) > 0 && stack[len(stack)-1].End <= s.Start {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			roots = append(roots, s)
		} else {
			parent := stack[len(stack)-1]
			children[parent] = append(children[parent], s)
		}
		stack = append(stack, s)
	}
	return roots
}

// writeRange writes the text from start to end, with each of sites, which
// lie in the range in source order, replaced by its form.
func (r *fileRenderer) writeRange(start, end int, sites []*enumerate.Site) {
	at := start
	for _, s := range sites {
		if s.Start < start || s.End > end {
			continue
		}
		r.writeText(at, s.Start)
		r.writeSite(s)
		at = s.End
	}
	r.writeText(at, end)
}

// writeText writes the text from start to end, with each edit that starts
// in that range or at its end applied. writeRange passes over the file's
// text once, in the order of the offsets, so each edit applies once, and an
// edit at a site's start precedes the site's form. No edit starts at the
// end of a node's range, and no edit ends after the end of the range. A
// form's own copy of an operand does not apply an edit. The forms copy an
// assignment's target, which contains no function, and the mutant's
// operand of an increment, whose other copy writeRange writes.
func (r *fileRenderer) writeText(start, end int) {
	for r.next < len(r.edits) && r.edits[r.next].start <= end {
		e := r.edits[r.next]
		r.buf.Write(r.text[start:e.start])
		r.buf.WriteString(e.text)
		start, r.next = e.end, r.next+1
	}
	r.buf.Write(r.text[start:end])
}

func (r *fileRenderer) off(pos token.Pos) int { return r.fset.File(pos).Offset(pos) }

// inner writes the node n of site s, with the sites nested in s.
func (r *fileRenderer) inner(s *enumerate.Site, n ast.Node) {
	r.writeRange(r.off(n.Pos()), r.off(n.End()), r.children[s])
}

// newlines writes a line break for each one between from and to, so a
// form keeps the line count of the text that it leaves out.
func (r *fileRenderer) newlines(from, to int) {
	r.buf.WriteString(strings.Repeat("\n", bytes.Count(r.text[from:to], []byte("\n"))))
}

func (r *fileRenderer) writef(format string, args ...any) { fmt.Fprintf(&r.buf, format, args...) }

// writeSite writes the form of s, which switches on the active mutant.
func (r *fileRenderer) writeSite(s *enumerate.Site) {
	begin := r.buf.Len()
	o := r.first[s]
	switch s.Form {
	case enumerate.Equality:
		r.writef("_mutateEq(%d, ", o)
		r.writeRange(s.Start, s.End, r.children[s])
		r.writef(")")
	case enumerate.Ordered, enumerate.Arithmetic:
		b := s.Node.(*ast.BinaryExpr)
		r.writef("_mutate_s%d%s(", o, typeArgs(s))
		r.inner(s, b.X)
		r.writef(", ")
		r.newlines(r.off(b.X.End()), r.off(b.Y.Pos()))
		r.inner(s, b.Y)
		r.writef(")")
	case enumerate.Compound:
		a := s.Node.(*ast.AssignStmt)
		target, value := a.Lhs[0], a.Rhs[0]
		source := r.text[r.off(target.Pos()):r.off(target.End())]
		r.buf.Write(source)
		r.writef(" = _mutate_s%d%s(%s, ", o, typeArgs(s), oneLine(source))
		r.newlines(r.off(target.End()), r.off(value.Pos()))
		r.inner(s, value)
		r.writef(")")
	case enumerate.Connector:
		b := s.Node.(*ast.BinaryExpr)
		var left, right, always int
		for i, m := range s.Mutants {
			switch m.Kind {
			case spec.LCRLeft:
				left = o + i
			case spec.LCRRight:
				right = o + i
			default:
				always = o + i
			}
		}
		c := and
		if b.Op == token.LOR {
			c = or
		}
		r.writef("(%s_mutateCT(%d, %d) %s (_mutateActive %s %d %s (", c.not, o, always, c.join, c.check, right, c.inner)
		r.inner(s, b.X)
		r.writef(")) %s ", c.join)
		r.newlines(r.off(b.X.End()), r.off(b.Y.Pos()))
		r.writef("(_mutateActive %s %d %s (", c.check, left, c.inner)
		r.inner(s, b.Y)
		r.writef(")))")
	case enumerate.IncDec:
		// The mutant's copy of the operand is on one line, so the form keeps
		// the operand's lines once, in the original's copy. The mutant's copy
		// runs only while the mutant is active, when no site in the operand
		// is, so it contains no form.
		st := s.Node.(*ast.IncDecStmt)
		source := r.text[r.off(st.X.Pos()):r.off(st.X.End())]
		r.writef("if _mutateIs(%d) { %s%s } else { ", o, oneLine(source), s.Mutants[0].Op)
		r.inner(s, st.X)
		r.writef("%s }", st.Tok)
	case enumerate.IncDecPost:
		st := s.Node.(*ast.IncDecStmt)
		source := r.text[r.off(st.X.Pos()):r.off(st.X.End())]
		r.buf.Write(source)
		r.writef(" = _mutate_s%d%s(%s)", o, typeArgs(s), oneLine(source))
	case enumerate.Not:
		r.writef("(")
		r.writeRange(s.Start, s.End, r.children[s])
		r.writef(" != _mutateIs(%d))", o)
	case enumerate.Minus:
		u := s.Node.(*ast.UnaryExpr)
		r.writef("_mutate_s%d%s(", o, typeArgs(s))
		r.newlines(s.Start, r.off(u.X.Pos()))
		r.inner(s, u.X)
		r.writef(")")
	case enumerate.Delete:
		r.writef("if !_mutateIs(%d) { ", o)
		r.writeRange(s.Start, s.End, r.children[s])
		r.writef(" }")
	case enumerate.Zero:
		r.writef("if _mutateIs(%d) { return %s }; ", o, r.zeros[s.Func])
		r.writeRange(s.Start, s.End, r.children[s])
	}
	r.spans = append(r.spans, span{start: begin, end: r.buf.Len(), site: s})
}

// oneLine returns the code of src on one line: its tokens separated by
// spaces, without comments. It writes each semicolon that the scanner
// inserts at a line break as ;, so the line is the same code as src, and
// leaves out the semicolon that the scanner inserts at the end of src. It
// writes a raw string literal that spans lines as the interpreted string
// literal of the same value.
func oneLine(src []byte) string {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var out []string
	inserted := false
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		// The scanner gives an inserted semicolon the literal "\n". Only a
		// raw string literal contains a line break, and src is code that
		// parses, so the literal unquotes.
		inserted = tok == token.SEMICOLON && lit == "\n"
		switch {
		case lit == "" || inserted:
			lit = tok.String()
		case tok == token.STRING && strings.Contains(lit, "\n"):
			value, _ := strconv.Unquote(lit)
			lit = strconv.Quote(value)
		}
		out = append(out, lit)
	}
	if inserted {
		out = out[:len(out)-1]
	}
	return strings.Join(out, " ")
}

// connectorForm is the text of a connector's form for one operator:
//
//	(not_mutateCT(o, always) join (_mutateActive check right inner (a)) join (_mutateActive check left inner (b)))
//
// For a && b it is (!always && (right || a) && (left || b)), and for a || b
// it is (always || (!right && a) || (!left && b)), where always, right and
// left report whether the site's mutant of that kind is active. With no
// mutant of the site active, the form evaluates as the original, in the
// same order. A mutant that leaves out an operand never evaluates it.
type connectorForm struct {
	not, join, check, inner string
}

// The forms of && and ||.
var (
	and = connectorForm{not: "!", join: "&&", check: "==", inner: "||"}
	or  = connectorForm{not: "", join: "||", check: "!=", inner: "&&"}
)

// typeArgs returns the explicit type argument list of a site's generic
// function, or nothing where the call infers it.
func typeArgs(s *enumerate.Site) string {
	if s.TypeArg == "" {
		return ""
	}
	return "[" + s.TypeArg + "]"
}

// languagePrefix starts every Go language version after Go 1, such as
// go1.21.
const languagePrefix = "go1."

// buildConstraint is the build constraint line that requires the language
// version go1.N, where N is its verb.
const buildConstraint = "//go:build " + languagePrefix + "%d\n"

// liftVersion returns the prefix of f's instrumented text, and f's text
// with each build constraint line before the package clause blanked. The
// prefix is a build constraint that requires go1.18 or the version that
// f's own constraint requires, whichever is later, and a line directive
// that numbers the line after it 1. The file is in the build, so the build
// satisfies its own constraint, and only the version that it implies stays.
func liftVersion(fset *token.FileSet, f *load.File) (string, []byte) {
	text := append([]byte(nil), f.Text...)
	minor := generics
	for _, group := range f.Syntax.Comments {
		if group.Pos() >= f.Syntax.Package {
			break
		}
		for _, c := range group.List {
			if !constraint.IsGoBuild(c.Text) && !constraint.IsPlusBuild(c.Text) {
				continue
			}
			if x, err := constraint.Parse(c.Text); err == nil && constraint.IsGoBuild(c.Text) {
				minor = max(minor, goMinor(constraint.GoVersion(x)))
			}
			start, end := fset.File(c.Pos()).Offset(c.Pos()), fset.File(c.End()).Offset(c.End())
			copy(text[start:end], bytes.Repeat([]byte(" "), end-start))
		}
	}
	return fmt.Sprintf(buildConstraint+"//line %s:1:1\n", minor, f.Path), text
}

// goMinor returns N of the version go1.N, and 0 for no version.
func goMinor(version string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(version, languagePrefix))
	return n
}
