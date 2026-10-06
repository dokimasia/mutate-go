// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"fmt"
	"go/token"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/spec"
)

// helperHead declares the switch, the trace and the helpers that the forms
// share. Its verbs are the build constraint, the package name, the
// protocol's variable, the trace's variable, the trace's first line and the
// length of the table of executed sites. It imports under names of its own,
// so no declaration of the package hides an import, and it writes the
// constants true and false as comparisons of literals, which no declaration
// of the package hides either.
//
// Each helper first checks, in a few instructions that the compiler
// inlines, whether the run traces or activates a mutant of its site, and
// otherwise computes the original. A function of its own, which the
// compiler does not inline, records the trace and switches on the mutant.
//
// A connector's form makes three checks. The first, _mutateCT, records the
// trace of the site, and the other two compare _mutateActive with an
// ordinal. The form passes no function value, so no operand escapes.
const helperHead = `%spackage %s

import (
	_mutateos "os"
	_mutatestrconv "strconv"
	_mutateatomic "sync/atomic"
)

var _mutateActive, _mutateTrace = _mutateStart()

var _mutateTracing = _mutateTrace != nil

func _mutateStart() (int, *_mutateos.File) {
	n, _ := _mutatestrconv.Atoi(_mutateos.Getenv(%q))
	path := _mutateos.Getenv(%q)
	if path == "" {
		return n, nil
	}
	f, err := _mutateos.OpenFile(path, _mutateos.O_WRONLY|_mutateos.O_APPEND|_mutateos.O_CREATE, 0o644)
	if err != nil {
		panic("mutate: " + err.Error())
	}
	_, _ = f.WriteString(%q)
	return n, f
}

var _mutateSeen [%d]uint32

func _mutateHit(o int) {
	if _mutateTracing && _mutateatomic.LoadUint32(&_mutateSeen[o]) == 0 &&
		_mutateatomic.CompareAndSwapUint32(&_mutateSeen[o], 0, 1) {
		_, _ = _mutateTrace.WriteString(_mutatestrconv.Itoa(o) + "\n")
	}
}

func _mutateIs(o int) bool {
	if _mutateTracing {
		_mutateHit(o)
	}
	return _mutateActive == o
}

func _mutateEq(o int, r bool) bool {
	if !_mutateTracing && uint(_mutateActive-o) >= 2 {
		return r
	}
	return _mutateEqSite(o, r)
}

func _mutateEqSite(o int, r bool) bool {
	_mutateHit(o)
	switch _mutateActive - o {
	case 0:
		return 0 == 0
	case 1:
		return 0 != 0
	}
	return r
}

func _mutateCT(o, n int) bool {
	if _mutateTracing {
		_mutateHit(o)
	}
	return _mutateActive == n
}

type _mutateInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type _mutateNumber interface {
	_mutateInteger | ~float32 | ~float64
}

type _mutateOrdered interface {
	_mutateNumber | ~string
}
`

// Imports returns the import paths that the helper file imports. The
// loader reads their export data, so the type checker can check the
// helper file.
func Imports() []string { return []string{"os", "strconv", "sync/atomic"} }

// helper returns the helper file of the package named name: the shared
// declarations of helperHead, which read the active mutant's ordinal from
// the variable variable, and the generic function of each site of sites
// that calls one. It returns the range of each site's function too. first
// maps each site to its first ordinal, and ordinals counts every ordinal of
// the program. When lift is true, the file requires go1.18.
func helper(
	name, variable string,
	sites []*enumerate.Site,
	first map[*enumerate.Site]int,
	ordinals int,
	lift bool,
) ([]byte, []span) {
	var b bytes.Buffer
	constraint := ""
	if lift {
		constraint = fmt.Sprintf(buildConstraint, generics) + "\n"
	}
	fmt.Fprintf(&b, helperHead, constraint, name, variable, TraceVar, traceStart+"\n", ordinals+1)
	var spans []span
	for _, s := range sites {
		begin := b.Len()
		o := first[s]
		switch s.Form {
		case enumerate.Ordered:
			original := "x " + s.Op.String() + " y"
			perSite(&b, o, "[T _mutateOrdered](x, y T) bool", "(x, y)", len(s.Mutants), original)
			fmt.Fprintf(&b, "\tswitch _mutateActive - %d {\n", o)
			for i, m := range s.Mutants {
				fmt.Fprintf(&b, "\tcase %d:\n\t\treturn %s\n", i, comparison(m))
			}
			fmt.Fprintf(&b, "\t}\n\treturn %s\n}\n", original)
		case enumerate.Arithmetic, enumerate.Compound:
			constraint := "_mutateNumber"
			if s.Op == token.REM {
				constraint = "_mutateInteger"
			}
			original := "x " + s.Op.String() + " y"
			perSite(&b, o, "[T "+constraint+"](x, y T) T", "(x, y)", 1, original)
			fmt.Fprintf(
				&b,
				"\tif _mutateActive == %d {\n\t\treturn x %s y\n\t}\n\treturn %s\n}\n",
				o,
				s.Mutants[0].Op,
				original,
			)
		case enumerate.IncDecPost:
			original := "x " + step(s.Op) + " 1"
			perSite(&b, o, "[T _mutateNumber](x T) T", "(x)", 1, original)
			fmt.Fprintf(
				&b,
				"\tif _mutateActive == %d {\n\t\treturn x %s 1\n\t}\n\treturn %s\n}\n",
				o,
				step(s.Mutants[0].Op),
				original,
			)
		case enumerate.Minus:
			perSite(&b, o, "[T _mutateNumber](x T) T", "(x)", 1, "-x")
			fmt.Fprintf(&b, "\tif _mutateActive == %d {\n\t\treturn x\n\t}\n\treturn -x\n}\n", o)
		default:
			continue
		}
		spans = append(spans, span{start: begin, end: b.Len(), site: s})
	}
	return b.Bytes(), spans
}

// perSite writes the generic function of the site whose first ordinal is o
// and whose n mutants follow it, and the start of the function that the
// first calls when the run traces or activates one of the n mutants. The
// first function returns original otherwise, and is small enough for the
// compiler to inline. signature is the type parameter list, the parameters
// and the result, and args the call's arguments. The caller writes the rest
// of the second function, whose body starts with the trace.
func perSite(b *bytes.Buffer, o int, signature, args string, n int, original string) {
	fmt.Fprintf(
		b,
		"\nfunc _mutate_s%d%s {\n\tif !_mutateTracing && uint(_mutateActive-%d) >= %d {\n\t\treturn %s\n\t}\n\treturn _mutate_s%dm%s\n}\n",
		o,
		signature,
		o,
		n,
		original,
		o,
		args,
	)
	fmt.Fprintf(b, "\nfunc _mutate_s%dm%s {\n\t_mutateHit(%d)\n", o, signature, o)
}

// comparison returns the expression that the mutant m of an ordered
// comparison returns: its operator between the operands, or a constant
// that no declaration of the package can hide.
func comparison(m *enumerate.Mutant) string {
	switch m.Kind {
	case spec.RORTrue:
		return trueExpr
	case spec.RORFalse:
		return falseExpr
	default:
		return "x " + m.Op.String() + " y"
	}
}

// step returns the operator that an increment or a decrement applies.
func step(op token.Token) string {
	if op == token.INC {
		return "+"
	}
	return "-"
}
