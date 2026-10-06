// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// keySeparator separates the fields of a key's digest, and of the group
// that counts a key's occurrences.
const keySeparator = "\x00"

// Form is the shape of a site, which decides its instrumented form.
type Form int

const (
	// Equality is a comparison with == or !=. Its mutants need only the
	// comparison's result.
	Equality Form = iota
	// Ordered is a comparison with <, <=, > or >=.
	Ordered
	// Arithmetic is a binary expression with +, -, *, / or %.
	Arithmetic
	// Compound is an assignment with +=, -=, *=, /= or %= whose target is
	// free of side effects.
	Compound
	// Connector is a binary expression with && or ||.
	Connector
	// IncDec is an increment or decrement statement in a statement list.
	IncDec
	// IncDecPost is an increment or decrement statement that is a for
	// loop's post statement, whose operand is free of side effects.
	IncDecPost
	// Not is a boolean operand, or a !x expression.
	Not
	// Minus is a unary minus of a number that is not a constant.
	Minus
	// Delete is a statement of a statement list.
	Delete
	// Zero is a return statement with results.
	Zero
)

// Status is what the enumeration alone decides about a mutant.
type Status int

const (
	// Runnable is a mutant whose verdict the run decides.
	Runnable Status = iota
	// Suppressed is a mutant that a rule family or an annotation
	// suppresses.
	Suppressed
	// NotViable is a mutant that the toolchain rejects.
	NotViable
	// NotSelected is a mutant outside the run's selection.
	NotSelected
)

// Position is a 1-based line and a 1-based column that counts bytes of
// UTF-8.
type Position struct {
	Line, Column int
}

// Range is one entry of a run's selection: the lines First to Last of
// File, a path relative to the module root with / as the separator.
type Range struct {
	File        string
	First, Last int
}

// Options are the settings of one enumeration.
type Options struct {
	// Lines is the run's selection. A nil selection selects every line, and
	// an empty one selects none.
	Lines []Range
	// IncludeGenerated makes every generated file of the package a target,
	// as if the comments before its package clause contained the include
	// directive.
	IncludeGenerated bool
}

// Site is one place in a source file that one or more mutants change.
type Site struct {
	File  *load.File
	Scope string
	// Start and End are byte offsets in File.Text. End is exclusive.
	Start, End int
	// StartPos and EndPos are the positions of Start and End.
	StartPos, EndPos Position
	Form             Form
	// Node is the expression or the statement of the site.
	Node ast.Node
	// Op is the site's operator: the binary operator, the operator of a
	// compound assignment without its =, or the increment or decrement.
	Op token.Token
	// TypeArg names the operands' type where the site's instrumented form
	// calls a generic function of its own, and the name denotes that type
	// at the site. It is empty where the call infers the type.
	TypeArg string
	// Zeros are the zero values of a Zero site's results, one per result.
	Zeros []string
	// Mutants lists the site's mutants in catalogue order.
	Mutants []*Mutant
}

// Mutant is one mutant of a site.
type Mutant struct {
	Site *Site
	Kind spec.Kind
	// Op is the operator that the mutant writes in place of the site's,
	// or token.ILLEGAL for a kind that writes no operator.
	Op token.Token
	// Key identifies the mutant across runs.
	Key        string
	Occurrence int
	// Original and Replacement are the site's source and the mutant's,
	// as a record states them.
	Original, Replacement string
	Status                Status
	// Rule is the rule family that suppresses the mutant.
	Rule spec.Family
	// Reason is the reason of the annotation that suppresses the mutant,
	// or why the toolchain rejects it.
	Reason string
}

// Skip is one site of a catalogue class that has no mutant.
type Skip struct {
	File       string
	Start, End Position
	Reason     spec.SkipReason
}

// Problem is a run error that the enumeration finds.
type Problem struct {
	Code    spec.ErrorCode
	Message string
}

// Result is a package's enumeration.
type Result struct {
	// Sites lists every site in source order: by file, by start, and the
	// longer of two sites with one start first. A compound assignment is
	// two sites with one range, the assignment and then its deletion.
	Sites []*Site
	// Mutants lists every mutant in the order of Sites.
	Mutants []*Mutant
	// Skipped lists every skipped site by file and start.
	Skipped []Skip
	// Generated lists each generated file of the package in file order.
	Generated []Generated
	Problems  []Problem
}

// Generated is a generated file of the package, the number of mutants that
// the kinds make at its sites before any exclusion, and whether the
// enumeration includes it.
type Generated struct {
	File     string
	Mutants  int
	Included bool
}

// Enumerate lists the mutants of p's source files under the definition d,
// with the selection and the generated files that opts states.
func Enumerate(p *load.Package, d spec.Definition, opts Options) *Result {
	e := &enumerator{
		pkg:          p,
		def:          d,
		include:      opts.IncludeGenerated,
		families:     map[string]spec.Family{},
		classes:      map[spec.Kind]spec.Class{},
		rank:         map[spec.Kind]int{},
		names:        map[string]bool{d.Catalogue.Every: true},
		suppressions: map[*load.File][]suppression{},
		covers:       map[fileLine][]*annotation{},
		calls:        map[*ast.CallExpr]spec.Family{},
		operands:     map[ast.Expr]bool{},
	}
	for family, rules := range d.Overlay.Families {
		for _, api := range rules.APIs {
			e.families[api] = family
		}
	}
	for i, k := range d.Catalogue.Kinds {
		e.classes[k.ID] = k.Class
		e.rank[k.ID] = i
		e.names[string(k.ID)], e.names[string(k.Class)] = true, true
	}
	e.results, e.unwritten = e.resultVariables(), e.unwrittenVariables()
	for _, f := range p.Files {
		if e.target(f) {
			e.file(f)
		}
	}
	r := e.result(opts.Lines)
	r.Generated = e.generated()
	return r
}

// target reports whether the enumeration walks f: a Source or a Cgo file
// that is not generated, or that is generated in an enumeration that
// includes generated files.
func (e *enumerator) target(f *load.File) bool {
	return f.Role != load.Support && (!f.Generated || e.include)
}

// generated returns each generated file of the package in file order, with
// the number of mutants that the kinds make at its sites, and whether the
// enumeration includes it. A second enumerator walks the files, so its
// count does not depend on the inclusion, and the result contains none of
// the sites, annotations and suppressions of a file that it leaves out.
func (e *enumerator) generated() []Generated {
	g := &enumerator{
		pkg: e.pkg, def: e.def, include: e.include, families: e.families, classes: e.classes, rank: e.rank,
		names: e.names, suppressions: map[*load.File][]suppression{}, covers: map[fileLine][]*annotation{},
		calls: map[*ast.CallExpr]spec.Family{}, operands: map[ast.Expr]bool{}, results: e.results,
		unwritten: e.unwritten,
	}
	out := []Generated{}
	for _, f := range e.pkg.Files {
		if f.Role == load.Support || !f.Generated {
			continue
		}
		before := len(g.sites)
		g.file(f)
		mutants := 0
		for _, s := range g.sites[before:] {
			mutants += len(s.Mutants)
		}
		out = append(out, Generated{File: f.Name, Mutants: mutants, Included: e.include})
	}
	return out
}

type enumerator struct {
	pkg *load.Package
	def spec.Definition
	// include reports whether the enumeration includes generated files.
	include  bool
	families map[string]spec.Family
	classes  map[spec.Kind]spec.Class
	rank     map[spec.Kind]int
	sites    []*Site
	skips    []skip
	// annotations lists every annotation in the order of the files and their
	// comments, and covers lists them by the line that each covers.
	// suppressions lists the suppressions of each file in the order that the
	// enumeration makes them.
	annotations  []*annotation
	covers       map[fileLine][]*annotation
	suppressions map[*load.File][]suppression
	problems     []Problem
	// names contains each name that an annotation can list: a kind, a
	// class, and the keyword of every kind.
	names map[string]bool
	// calls maps each call that a family which lists calls suppresses to
	// the family. operands contains each operand, without its parentheses,
	// of a connector that is a site. results maps each variable whose calls
	// a result rule puts into a family to the family. unwritten contains
	// each variable that unwrittenVariables returns.
	calls     map[*ast.CallExpr]spec.Family
	operands  map[ast.Expr]bool
	results   map[*types.Var]spec.Family
	unwritten map[*types.Var]bool
}

type skip struct {
	f          *load.File
	start, end int
	reason     spec.SkipReason
}

func (e *enumerator) off(pos token.Pos) int { return e.pkg.Fset.File(pos).Offset(pos) }

func (e *enumerator) text(f *load.File, n ast.Node) string {
	return string(f.Text[e.off(n.Pos()):e.off(n.End())])
}

// position returns the position of offset in f. A Cgo file's line
// directives map its offsets to the file that imports C.
func (e *enumerator) position(f *load.File, offset int) Position {
	tf := e.pkg.Fset.File(f.Syntax.Package)
	p := tf.PositionFor(tf.Pos(offset), f.Role == load.Cgo)
	return Position{Line: p.Line, Column: p.Column}
}

// file walks the declarations of f, and then suppresses the compound
// statements that a family which lists calls suppresses as a whole.
func (e *enumerator) file(f *load.File) {
	e.parseAnnotations(f)
	for _, decl := range f.Syntax.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			e.walk(f, d, scopeOf(d), d.Type)
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				switch s := sp.(type) {
				case *ast.ValueSpec:
					if s.Type != nil {
						e.walk(f, s.Type, s.Names[0].Name, nil)
					}
					for i, v := range s.Values {
						e.walk(f, v, s.Names[min(i, len(s.Names)-1)].Name, nil)
					}
				case *ast.TypeSpec:
					e.walk(f, s.Type, s.Name.Name, nil)
				}
			}
		}
	}
	e.quietStatements(f)
}

// scopeOf names a function F as F, and a method M of T or *T as T.M. The
// package type-checks, so a method's receiver names a type that the
// package declares, possibly in parentheses and with type parameters.
func scopeOf(d *ast.FuncDecl) string {
	if d.Recv == nil {
		return d.Name.Name
	}
	t := ast.Unparen(d.Recv.List[0].Type)
	if star, ok := t.(*ast.StarExpr); ok {
		t = ast.Unparen(star.X)
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	return t.(*ast.Ident).Name + "." + d.Name.Name
}

func (e *enumerator) result(lines []Range) *Result {
	order := map[*load.File]int{}
	for i, f := range e.pkg.Files {
		order[f] = i
	}
	// The engine cannot instrument a file that imports C, so each of its
	// sites is a skipped site.
	sites := e.sites[:0]
	for _, s := range e.sites {
		if s.File.Role == load.Cgo {
			e.skips = append(e.skips, skip{f: s.File, start: s.Start, end: s.End, reason: spec.SkipCgo})
			continue
		}
		sites = append(sites, s)
	}
	e.sites = sites
	sort.SliceStable(e.sites, func(i, j int) bool {
		a, b := e.sites[i], e.sites[j]
		if a.File != b.File {
			return order[a.File] < order[b.File]
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.End > b.End
	})
	r := &Result{Sites: e.sites, Mutants: []*Mutant{}, Skipped: []Skip{}}
	occurrences := map[string]int{}
	rules := map[*load.File]*ruleIndex{}
	selection := lineIndex(lines)
	for _, s := range e.sites {
		sort.SliceStable(
			s.Mutants,
			func(i, j int) bool { return e.rank[s.Mutants[i].Kind] < e.rank[s.Mutants[j].Kind] },
		)
		s.StartPos, s.EndPos = e.position(s.File, s.Start), e.position(s.File, s.End)
		tokens := Tokens(s.File.Text[s.Start:s.End])
		if rules[s.File] == nil {
			rules[s.File] = newRuleIndex(e.suppressions[s.File])
		}
		rule := rules[s.File].rule(s)
		for _, m := range s.Mutants {
			group := strings.Join([]string{s.File.Name, s.Scope, string(m.Kind), tokens}, keySeparator)
			m.Occurrence = occurrences[group]
			occurrences[group]++
			m.Key = Key(e.def.Catalogue.Key, s.File.Name, s.Scope, m.Kind, tokens, m.Occurrence)
			m.Original, m.Replacement = Cut(string(s.File.Text[s.Start:s.End]), m.Replacement)
			// Every mutant meets the annotations, so an annotation that
			// suppresses only mutants outside the selection is not stale. A
			// mutant outside the selection is not-selected, whatever else
			// excludes it, and states no rule or reason.
			e.suppress(m, rule)
			if !selected(selection, s.File.Name, s.StartPos.Line) {
				m.Status, m.Rule, m.Reason = NotSelected, "", ""
			}
			r.Mutants = append(r.Mutants, m)
		}
	}
	for _, a := range e.annotations {
		if !a.used {
			e.problems = append(e.problems, Problem{Code: spec.ErrorStale, Message: a.stale()})
		}
	}
	sort.SliceStable(e.skips, func(i, j int) bool {
		a, b := e.skips[i], e.skips[j]
		if a.f != b.f {
			return order[a.f] < order[b.f]
		}
		return a.start < b.start
	})
	for i, sk := range e.skips {
		// Two classes at one place of a file that imports C give one entry.
		if i > 0 && sk == e.skips[i-1] {
			continue
		}
		r.Skipped = append(
			r.Skipped,
			Skip{File: sk.f.Name, Start: e.position(sk.f, sk.start), End: e.position(sk.f, sk.end), Reason: sk.reason},
		)
	}
	r.Problems = e.problems
	return r
}

// lineIndex returns the ranges of a selection by file, sorted by their first
// lines and merged where they overlap or meet, so selected finds the range
// of a line with a binary search. It returns nil for a nil selection, which
// selects every line, and an empty map for an empty one, which selects
// none.
func lineIndex(lines []Range) map[string][]Range {
	if lines == nil {
		return nil
	}
	byFile := map[string][]Range{}
	for _, r := range lines {
		byFile[r.File] = append(byFile[r.File], r)
	}
	for file, ranges := range byFile {
		slices.SortFunc(ranges, func(a, b Range) int { return cmp.Compare(a.First, b.First) })
		merged := ranges[:1]
		for _, r := range ranges[1:] {
			if last := &merged[len(merged)-1]; r.First <= last.Last+1 {
				last.Last = max(last.Last, r.Last)
			} else {
				merged = append(merged, r)
			}
		}
		byFile[file] = merged
	}
	return byFile
}

// selected reports whether a site that starts on line of file lies in the
// selection that index, a lineIndex, states.
func selected(index map[string][]Range, file string, line int) bool {
	if index == nil {
		return true
	}
	ranges := index[file]
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].First > line }) - 1
	return i >= 0 && line <= ranges[i].Last
}

// typeName returns a name for t that resolves in every file of the package
// without a new import: a predeclared type, or a type without type
// parameters that the package declares at package level. The type checker
// gives every operand of a site's arithmetic or comparison a type, so a
// basic type is a predeclared number or string.
func (e *enumerator) typeName(t types.Type) (string, bool) {
	switch x := types.Unalias(t).(type) {
	case *types.Basic:
		return x.Name(), true
	case *types.Named:
		obj := x.Obj()
		if obj.Pkg() == e.pkg.Types && x.TypeArgs().Len() == 0 && obj.Parent() == e.pkg.Types.Scope() {
			return obj.Name(), true
		}
	}
	return "", false
}

// typeArg returns the name of t when typeName names it and the name denotes
// t at pos, where a local declaration can hide it.
func (e *enumerator) typeArg(t types.Type, pos token.Pos) string {
	name, ok := e.typeName(t)
	if !ok {
		return ""
	}
	_, obj := e.pkg.Types.Scope().Innermost(pos).LookupParent(name, pos)
	if tn, ok := obj.(*types.TypeName); !ok || !types.Identical(tn.Type(), t) {
		return ""
	}
	return name
}
