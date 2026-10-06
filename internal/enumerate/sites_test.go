// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/spec"
)

const kinds = `package fixture

func kinds(a, b int, ok bool, xs []int) int {
	total := a + b
	total -= b
	if a < b && ok {
		total++
	}
	if xs == nil || a >= b {
		return -a
	}
	if !ok {
		return total
	}
	return 0
}
`

const skips = `package fixture

import "time"

const big = 4*1024 + 1

var buf [2 * 8]byte

type flag bool

func generic[T int | float64](a, b T) T {
	return a + b
}

func named(n int) flag {
	return n < 4
}

func both(n int) flag {
	return n > 0 && n < 9
}

func recovers(xs []int) bool {
	return len(xs) > 0 && recover() == nil
}

func target(xs []int) {
	xs[index()] += 1
}

func index() int { return 0 }

func shift(d time.Duration, n uint) bool {
	return d < 1<<n
}
`

// forms names each form of a site, by its value.
var forms = []string{
	"Equality", "Ordered", "Arithmetic", "Compound", "Connector", "IncDec",
	"IncDecPost", "Not", "Minus", "Delete", "Zero",
}

func TestSites(t *testing.T) {
	t.Parallel()

	t.Run("Enumerate", func(t *testing.T) {
		t.Parallel()

		t.Run("makes every kind in source order", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"kinds.go": kinds})
			assert.Equal(t, listing(r, ""), `kinds aor 0: a + b -> "a - b"
kinds aor 1: total -= b -> "total += b"
kinds sbr-delete 0: total -= b -> ""
kinds sbr-delete 1: if a < b && ok { total++ } -> ""
kinds lcr-left 0: a < b && ok -> "a < b"
kinds lcr-right 0: a < b && ok -> "ok"
kinds lcr-false 0: a < b && ok -> "false"
kinds ror-boundary 0: a < b -> "a <= b"
kinds ror-false 0: a < b -> "false"
kinds uoi-incdec 0: total++ -> "total--"
kinds sbr-delete 2: if xs == nil || a >= b { return -a } -> ""
kinds lcr-left 1: xs == nil || a >= b -> "xs == nil"
kinds lcr-right 1: xs == nil || a >= b -> "a >= b"
kinds lcr-true 0: xs == nil || a >= b -> "true"
kinds ror-true 0: xs == nil -> "true"
kinds ror-false 1: xs == nil -> "false"
kinds ror-boundary 1: a >= b -> "a > b"
kinds ror-true 1: a >= b -> "true"
kinds sbr-zero 0: return -a -> "return 0"
kinds uoi-minus 0: -a -> "a"
kinds sbr-delete 3: if !ok { return total } -> ""
kinds uoi-not 0: !ok -> "ok"
kinds sbr-zero 1: return total -> "return 0"
`, "each kind's mutants are in source order")
			expect.That(t, r.Skipped).Empty("no site is skipped")
			expect.Empty(t, r.Problems, "no annotation fails")
			expect.Empty(t, statuses(r), "every mutant runs")
			s := r.Mutants[1].Site
			expect.Equal(t, s.File.Name, "kinds.go", "the compound assignment is in kinds.go")
			expect.Equal(t, s.StartPos, enumerate.Position{Line: 5, Column: 2}, "and starts at its target")
			expect.Equal(t, s.EndPos, enumerate.Position{Line: 5, Column: 12}, "and ends after its value")
		})

		t.Run("states each site's form, operator, type argument and zero values", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"kinds.go": kinds})
			var b strings.Builder
			for _, s := range r.Sites {
				fmt.Fprintf(&b, "%s %s %q %q %v\n", s.Scope, forms[s.Form], s.Op, s.TypeArg, s.Zeros)
			}
			expect.Equal(t, b.String(), `kinds Arithmetic "+" "int" []
kinds Compound "-" "int" []
kinds Delete "ILLEGAL" "" []
kinds Delete "ILLEGAL" "" []
kinds Connector "&&" "" []
kinds Ordered "<" "int" []
kinds IncDec "++" "" []
kinds Delete "ILLEGAL" "" []
kinds Connector "||" "" []
kinds Equality "==" "" []
kinds Ordered ">=" "int" []
kinds Zero "ILLEGAL" "" [0]
kinds Minus "-" "int" []
kinds Delete "ILLEGAL" "" []
kinds Not "ILLEGAL" "" []
kinds Zero "ILLEGAL" "" [0]
`, "each site states the facts of its instrumented form")
			var ops []string
			for _, m := range r.Mutants {
				if m.Op != token.ILLEGAL {
					ops = append(ops, string(m.Kind)+" "+m.Op.String())
				}
			}
			expect.Equal(t, ops, []string{"aor -", "aor +", "ror-boundary <=", "uoi-incdec --", "ror-boundary >"},
				"and each mutant that writes an operator states it")
		})

		t.Run("names the operands' type only where the name denotes it at the site", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"names.go": `package fixture

import "time"

type size int

type alias = size

func names(a, b size, c, d alias, e, f time.Duration, g, h struct{ n int }) bool {
	{
		type size uint
		var x, y size
		_ = x < y
		_ = a < b
	}
	{
		var int float64
		var p, q = 1, 2
		_, _ = int, p < q
	}
	for i := 0; i < 3; i++ {
	}
	return a < b && c < d && e < f && g.n < h.n
}

func local() bool {
	type size int
	var x, y size
	return x < y
}
`})
			var b strings.Builder
			for _, s := range r.Sites {
				if s.Form == enumerate.Ordered || s.Form == enumerate.IncDecPost {
					fmt.Fprintf(&b, "%s %q\n", s.File.Text[s.Start:s.End], s.TypeArg)
				}
			}
			assert.Equal(t, b.String(), `x < y ""
a < b ""
p < q ""
i < 3 "int"
i++ "int"
a < b "size"
c < d "size"
e < f ""
g.n < h.n "int"
x < y ""
`, "a site names its operands' type where a local declaration does not hide the name")
		})

		t.Run("names a function F as F and a method M of T or *T as T.M", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"scopes.go": `package fixture

var limit = 1 + compute(3)

func compute(n int) int { return n }

type box[T any] struct{ n int }

func (b *box[T]) grow(by int) int {
	f := func() int { return b.n + by }
	return f()
}

type pair[K, V any] struct{ k *K }

func (p pair[K, V]) key() bool { return p.k == nil }

var width, depth = compute(1), 2 + compute(1)

type counter struct{ n int }

func (c (counter)) get() int { return c.n + 1 }

func (c *(counter)) put(by int) { c.n += by }
`})
			assert.Equal(t, listing(r, ""), `limit aor 0: 1 + compute(3) -> "1 - compute(3)"
compute sbr-zero 0: return n -> "return 0"
box.grow sbr-zero 0: return b.n + by -> "return 0"
box.grow aor 0: b.n + by -> "b.n - by"
box.grow sbr-zero 1: return f() -> "return 0"
pair.key sbr-zero 0: return p.k == nil -> "return false"
pair.key ror-true 0: p.k == nil -> "true"
pair.key ror-false 0: p.k == nil -> "false"
depth aor 0: 2 + compute(1) -> "2 - compute(1)"
counter.get sbr-zero 0: return c.n + 1 -> "return 0"
counter.get aor 0: c.n + 1 -> "c.n - 1"
counter.put aor 0: c.n += by -> "c.n -= by"
counter.put sbr-delete 0: c.n += by -> ""
`, "each site has the scope of its declaration")
		})

		t.Run("lists the sites that the overlay skips with their reasons", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"skips.go": skips})
			expect.Equal(t, skipped(r, skips), `4*1024 + 1: constant expression
2 * 8: constant expression
a + b: operand of type-parameter type
n < 4: named boolean result
n > 0 && n < 9: named boolean result
n > 0: named boolean result
n < 9: named boolean result
xs[index()] += 1: assignment target with side effects
d < 1<<n: untyped constant in a non-constant shift
`, "each skipped site states the overlay's reason")
			expect.Equal(t, listing(r, "lcr-"), `recovers lcr-left 0: len(xs) > 0 && recover() == nil -> "len(xs) > 0"
recovers lcr-right 0: len(xs) > 0 && recover() == nil -> "recover() == nil"
recovers lcr-false 0: len(xs) > 0 && recover() == nil -> "false"
`, "and a connector of plain booleans keeps its mutants")
		})

		t.Run("lists the outermost constant expression and makes no mutant of other values", func(t *testing.T) {
			t.Parallel()
			src := `package fixture

const ok = 1 < 2 && true

const name = "a" + "b"

const shift = 1 << 3

func f(s string, c complex128, x int) (string, complex128, int) {
	s += "x"
	c++
	c = -c
	x = -1 + x
	return s + "y", c, x
}
`
			r := all(t, map[string]string{"values.go": src})
			expect.Equal(t, skipped(r, src), "1 < 2 && true: constant expression\n",
				"only the constant connector is a skipped site")
			expect.Equal(t, listing(r, ""), `f sbr-delete 0: s += "x" -> ""
f sbr-delete 1: c = -c -> ""
f sbr-delete 2: x = -1 + x -> ""
f aor 0: -1 + x -> "-1 - x"
f sbr-zero 0: return s + "y", c, x -> "return \"\", 0, 0"
`, "and strings and complex numbers have no arithmetic mutant")
		})

		t.Run("returns zero values only where a result is not zero already", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"results.go": `package fixture

func results(n int) (bool, string, error) {
	if n > 0 {
		return true, "", nil
	}
	if n < 0 {
		return false, "x", nil
	}
	return false, "", nil
}
`})
			assert.Equal(
				t,
				listing(r, string(spec.SBRZero)),
				`results sbr-zero 0: return true, "", nil -> "return false, \"\", nil"
results sbr-zero 1: return false, "x", nil -> "return false, \"\", nil"
`,
				"the return of zero values has no mutant",
			)
		})

		t.Run("makes no zero mutant of a return whose every result is a zero value", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"zero.go": `package fixture

type point struct{ x, y int }

func structs(ok bool) (point, bool) {
	if ok {
		return point{x: 0}, false
	}
	return (point{}), false
}

func arrays() ([2]point, *int) {
	return [2]point{{}, {y: 0}}, nil
}

func news[T any]() (T, string) {
	return *new(T), ""
}

func (p *point) self() *point { return p }

func slice() []int { return []int{} }

func table() map[int]int { return map[int]int{} }

func one() point { return point{x: 1} }

func deref(p *point) point { return *(p) }

func call(p *point) point { return *p.self() }
`})
			assert.Equal(t, listing(r, string(spec.SBRZero)), `point.self sbr-zero 0: return p -> "return nil"
slice sbr-zero 0: return []int{} -> "return nil"
table sbr-zero 0: return map[int]int{} -> "return nil"
one sbr-zero 0: return point{x: 1} -> "return point{}"
deref sbr-zero 0: return *(p) -> "return point{}"
call sbr-zero 0: return *p.self() -> "return point{}"
`, "only the return of a value that is no zero value has the mutant")
		})

		t.Run("makes a zero mutant of a return of a value that is no interface as an interface", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"boxed.go": `package fixture

type pair struct {
	label any
	n     int
}

func answer() any { return 0 }

func empty() any { return "" }

func none() error { return nil }

func boxed() any { return *new(any) }

func labelled() pair { return pair{label: ""} }

func positional() pair { return pair{nil, 0} }

func list() [2]any { return [2]any{nil, false} }

func generic[T any]() T { return *new(T) }

func split() (any, error) { return nil, nil }

func forward() (any, error) { return split() }
`})
			assert.Equal(t, listing(r, string(spec.SBRZero)), `answer sbr-zero 0: return 0 -> "return nil"
empty sbr-zero 0: return "" -> "return nil"
labelled sbr-zero 0: return pair{label: ""} -> "return pair{}"
list sbr-zero 0: return [2]any{nil, false} -> "return [2]any{}"
forward sbr-zero 0: return split() -> "return nil, nil"
`, "a 0, an empty string and false in an interface are no nil interface, and a call's results are no zero value")
		})

		t.Run("makes no zero mutant of a return of a variable that no use writes", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"unwritten.go": `package fixture

import "strconv"

type point struct{ x, y int }

func (p *point) shift() { p.x++ }

func (p point) sum() int { return p.x + p.y }

func index[T any](s []T, i int) (T, bool) {
	if i < len(s) {
		return s[i], true
	}
	var zero T
	return zero, false
}

func values() (point, error, [2]int) {
	var (
		p   point
		err error
		a   [2]int
	)
	_, _, _ = p.sum(), len(a), a[0]
	return p, err, a
}

func boxed() any {
	var n int
	return n
}

var global int

func pkg() int { return global }

func assign() int { var n int; n = 1; return n }

func compound() int { var n int; n += 1; return n }

func paren() int { var n int; (n) = 1; return n }

func field() point { var p point; p.x = 1; return p }

func element() [2]int { var a [2]int; a[0] = 1; return a }

func declare(s string) (int, error) { var n int; n, err := strconv.Atoi(s); return n, err }

func increment() int { var n int; n++; return n }

func ranged(xs []int) int { var x int; for _, x = range xs {}; return x }

func address() int { var n int; p := &n; *p = 1; return n }

func slice() [2]byte { var a [2]byte; copy(a[:], "ab"); return a }

func method() point { var p point; p.shift(); return p }

func value() point { var p point; f := p.shift; f(); return p }

func pointer() *int { var p *int; *p = 1; return p }

func closure() int { var n int; func() { n = 1 }(); return n }

func later(xs []int) int {
	var x int
	for i, v := range xs {
		if i > 0 {
			return x
		}
		x = v
	}
	return 0
}

func fresh() *point { return &point{} }
`})
			assert.Equal(t, listing(r, string(spec.SBRZero)), `point.sum sbr-zero 0: return p.x + p.y -> "return 0"
index sbr-zero 0: return s[i], true -> "return *new(T), false"
boxed sbr-zero 0: return n -> "return nil"
pkg sbr-zero 0: return global -> "return 0"
assign sbr-zero 0: return n -> "return 0"
compound sbr-zero 0: return n -> "return 0"
paren sbr-zero 0: return n -> "return 0"
field sbr-zero 0: return p -> "return point{}"
element sbr-zero 0: return a -> "return [2]int{}"
declare sbr-zero 0: return n, err -> "return 0, nil"
increment sbr-zero 0: return n -> "return 0"
ranged sbr-zero 0: return x -> "return 0"
address sbr-zero 0: return n -> "return 0"
slice sbr-zero 0: return a -> "return [2]byte{}"
method sbr-zero 0: return p -> "return point{}"
value sbr-zero 0: return p -> "return point{}"
pointer sbr-zero 0: return p -> "return nil"
closure sbr-zero 0: return n -> "return 0"
later sbr-zero 0: return x -> "return 0"
fresh sbr-zero 0: return &point{} -> "return nil"
`, "a return of a local variable that a use writes is a site, and one of a variable that no use writes is not")
		})

		t.Run("makes a zero mutant of a return of the value that new of an expression points to", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{
				goMod:    "module fixture\n\ngo 1.26\n",
				"new.go": "package fixture\n\nfunc pointed(x int) (int, int) {\n\treturn *new(x), *new(int)\n}\n",
			})
			assert.Equal(
				t,
				listing(r, string(spec.SBRZero)),
				"pointed sbr-zero 0: return *new(x), *new(int) -> \"return 0, 0\"\n",
				"*new(x) of an expression is the value of x, which is no zero value",
			)
		})

		t.Run("writes each zero value as Go code writes it", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"spell.go": `package fixture

import (
	"time"
	"unsafe"
)

type name string

type pair struct{ a, b int }

func all(n int, f float64, c complex128, s string, ok bool, d time.Duration, nm name) (int, float64, complex128, string, bool, time.Duration, name) {
	return n, f, c, s, ok, d, nm
}

func refs(p *int, xs []int, m map[int]int, ch chan int, fn func(), e error, u unsafe.Pointer) (*int, []int, map[int]int, chan int, func(), error, unsafe.Pointer) {
	return p, xs, m, ch, fn, e, u
}

func composites(p pair, a [3]int, s struct{ x int }) (pair, ([3]int), struct{ x int }) {
	return p, a, s
}

func generic[T any](v T) (T, []T) {
	return v, nil
}
`})
			assert.Equal(t, listing(r, string(spec.SBRZero)),
				`all sbr-zero 0: return n, f, c, s, ok, d, nm -> "return 0, 0, 0, \"\", false, 0, \"\""
refs sbr-zero 0: return p, xs, m, ch, fn, e, u -> "return nil, nil, nil, nil, nil, nil, nil"
composites sbr-zero 0: return p, a, s -> "return pair{}, [3]int{}, struct{ x int }{}"
generic sbr-zero 0: return v, nil -> "return *new(T), nil"
`, "each zero value reads as Go code")
		})

		t.Run("makes no increment mutant of an increment in a statement's initializer", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"inits.go": `package fixture

func inits(n int) int {
	for n++; n < 3; {
		n += 2
	}
	if n--; n > 0 {
		return n
	}
	return 0
}
`})
			assert.Empty(t, listing(r, string(spec.UOIIncDec)), "an increment in an initializer has no mutant")
		})

		t.Run("skips an equality whose result has a named boolean type", func(t *testing.T) {
			t.Parallel()
			src := `package fixture

type flag bool

func same(a, b int) flag {
	return a == b
}
`
			r := all(t, map[string]string{"same.go": src})
			assert.Equal(t, skipped(r, src), "a == b: named boolean result\n", "the equality is skipped")
		})

		t.Run(
			"skips arithmetic on a shifted untyped constant whose type has no name in the package",
			func(t *testing.T) {
				t.Parallel()
				src := `package fixture

import (
	"math"
	"time"
)

const one = 1

func shifts(d time.Duration, n uint, f func() int) bool {
	_ = d + one<<n
	_ = d + math.MaxInt8<<n
	_ = d + -1<<n
	_ = d + (1+1)<<n
	_ = d + min(1, 2)<<n
	_ = d + time.Duration(f())<<n
	return d < d<<n
}
`
				r := all(t, map[string]string{"shifts.go": src})
				expect.Equal(t, skipped(r, src), `d + one<<n: untyped constant in a non-constant shift
d + math.MaxInt8<<n: untyped constant in a non-constant shift
d + -1<<n: untyped constant in a non-constant shift
d + (1+1)<<n: untyped constant in a non-constant shift
1+1: constant expression
d + min(1, 2)<<n: untyped constant in a non-constant shift
`, "each shift of an untyped constant skips its arithmetic")
				expect.Equal(
					t,
					listing(r, string(spec.AOR)),
					"shifts aor 0: d + time.Duration(f())<<n -> \"d - time.Duration(f())<<n\"\n",
					"and a shift of a typed operand keeps it",
				)
				expect.Equal(
					t,
					listing(r, string(spec.RORBoundary)),
					"shifts ror-boundary 0: d < d<<n -> \"d <= d<<n\"\n",
					"as does a comparison with a shift of a variable",
				)
			},
		)

		t.Run("skips arithmetic on a type parameter whose every type is a number", func(t *testing.T) {
			t.Parallel()
			src := `package fixture

type exact interface{ int }

type integer interface{ ~int | ~int64 }

type number interface{ integer | ~float64 }

func tps[T exact, N number](a, b T, c, d N) (T, N) {
	return a + b, c - d
}
`
			r := all(t, map[string]string{"tps.go": src})
			expect.Equal(t, skipped(r, src), `a + b: operand of type-parameter type
c - d: operand of type-parameter type
`, "the type set of a single type and of an embedded constraint are numbers")
			expect.Equal(t, listing(r, ""), "tps sbr-zero 0: return a + b, c - d -> \"return *new(T), *new(N)\"\n",
				"and the return keeps its mutant")
		})

		t.Run("makes no mutant of arithmetic on a type parameter whose type set has a string", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"mixed.go": `package fixture

type mixed interface{ ~int | ~string }

func mix[U mixed](c, d U) U {
	c += d
	return c
}
`})
			expect.Equal(t, listing(r, ""), `mix sbr-delete 0: c += d -> ""
mix sbr-zero 0: return c -> "return *new(U)"
`, "the compound assignment has no aor mutant")
			expect.Empty(t, r.Skipped, "and no skipped site")
		})

		t.Run("skips the sites of type-parameter operands and side-effect targets of every kind", func(t *testing.T) {
			t.Parallel()
			src := `package fixture

type number interface{ ~int | ~float64 }

type any2 interface{ int | string }

func generic[T number](a, b T, ys []int, q *int) bool {
	a += b
	for i := a; i < b; i++ {
		_ = -i
	}
	for i := 0; i < 2; ys[i]++ {
	}
	for ; a < b; *q++ {
	}
	return a < b
}

func mixed[T any2](a T) bool {
	var x T
	return a == x
}

func shifted(n uint) uint {
	return 1<<n + 1
}
`
			r := all(t, map[string]string{"generic.go": src})
			expect.Equal(t, skipped(r, src), `a += b: operand of type-parameter type
i < b: operand of type-parameter type
i++: operand of type-parameter type
-i: operand of type-parameter type
ys[i]++: assignment target with side effects
a < b: operand of type-parameter type
*q++: assignment target with side effects
a < b: operand of type-parameter type
`, "each site of a type parameter or of a target with side effects is skipped")
			expect.Equal(t, listing(r, string(spec.AOR)), "shifted aor 0: 1<<n + 1 -> \"1<<n - 1\"\n",
				"and arithmetic of a predeclared type keeps its mutant")
		})

		t.Run("negates only boolean values in positions that take a value", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"positions.go": `package fixture

type pair struct{ ok bool }

func positions(m map[string]bool, p *pair, ch chan bool, f func() bool) bool {
	v, found := m["k"]
	var w, present = m["j"]
	p.ok = v
	q := &p.ok
	_ = map[bool]int{found: 1}
	_ = pair{ok: present}
	f()
	ch <- *q
	defer f()
	return !w
}
`})
			assert.Equal(t, listing(r, string(spec.UOINot)), `positions uoi-not 0: v -> "!v"
positions uoi-not 1: present -> "!present"
positions uoi-not 2: *q -> "!*q"
positions uoi-not 3: !w -> "w"
`, "a value of bool is negated where the negation compiles and changes the program")
		})

		t.Run("negates a boolean in parentheses only where the boolean without them is negated", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"parens.go": `package fixture

func parens(b *bool, ok bool, m map[int]bool) (*bool, bool) {
	(*b) = true
	(ok) = m[1]
	p := &(ok)
	_ = map[bool]int{(ok): 1}
	_ = !(ok)
	return p, ((ok))
}
`})
			assert.Equal(t, listing(r, string(spec.UOINot)), `parens uoi-not 0: m[1] -> "!m[1]"
parens uoi-not 1: !(ok) -> "(ok)"
parens uoi-not 2: ok -> "!ok"
`, "an assignment's target, an operand of & or ! and a key keep their position in parentheses")
		})

		t.Run("keeps the statements that a deletion would break", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"ends.go": `package fixture

func ends(n int) (int, error) {
	if n > 0 {
		return 0, nil
	}
	if n > 1 {
	loop:
		for {
			break loop
		}
	}
	go func() {}()
	panic("unreachable")
}
`})
			assert.Equal(t, listing(r, "sbr-"), `ends sbr-delete 0: if n > 0 { return 0, nil } -> ""
ends sbr-delete 1: go func() {}() -> ""
`, "a statement with a label and a terminating final statement are kept")
		})

		// The mutant's source keeps the code that it leaves out behind a
		// constant that skips it, so the compiler still sees every use.
		leftOut := []struct {
			name, file, src, prefix, want string
		}{
			{
				name: "marks a deletion that leaves out the only use of a name runnable",
				file: "unused.go",
				src: `package fixture

import (
	"context"
	"os"
	"sort"
	"time"
)

var log []string

func keep(s string, d time.Duration) (n int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	os.Args = nil
	_ = os.Getpid()
	total := 0
	total += len(s)
	log = append(log, s)
	sort.Strings(log)
	f := func() { total++ }
	f()
	var last string
	last = s
	_ = last
	for _, x := range log {
		n += len(x)
	}
	var y string
	for _, y = range log {
	}
	println(y)
	<-ctx.Done()
	return total + n
}
`,
				prefix: string(spec.SBRDelete),
				want: `keep sbr-delete 0: defer cancel() -> ""
keep sbr-delete 1: os.Args = nil -> ""
keep sbr-delete 2: total += len(s) -> ""
keep sbr-delete 3: log = append(log, s) -> ""
keep sbr-delete 4: sort.Strings(log) -> ""
keep sbr-delete 5: f() -> ""
keep sbr-delete 6: last = s -> ""
keep sbr-delete 7: for _, x := range log { n += len(x) } -> ""
keep sbr-delete 8: n += len(x) -> ""
keep sbr-delete 9: for _, y = range log { } -> ""
keep sbr-delete 10: println(y) -> ""
keep sbr-delete 11: <-ctx.Done() -> ""
`,
			},
			{
				name: "marks a deletion that leaves out the only use of a type switch's symbol runnable",
				file: "switch.go",
				src: `package fixture

func only(v any) int {
	n := 0
	switch x := v.(type) {
	case string:
		n = len(x)
	case int:
		n = 1
	}
	return n
}
`,
				prefix: string(spec.SBRDelete),
				want: `only sbr-delete 0: switch x := v.(type) { case string: n = len(x) case int: n = 1 } -> ""
only sbr-delete 1: n = len(x) -> ""
only sbr-delete 2: n = 1 -> ""
`,
			},
			{
				name: "marks a deletion that leaves out the only use of a label runnable",
				file: "labels.go",
				src: `package fixture

func first(xs []int) int {
	i := 0
scan:
	for ; i < len(xs); i++ {
		if xs[i] < 0 {
			break scan
		}
	}
	return i
}
`,
				prefix: string(spec.SBRDelete),
				want:   "first sbr-delete 0: if xs[i] < 0 { break scan } -> \"\"\n",
			},
			{
				name: "marks a connector mutant that leaves out the only use of a name runnable",
				file: "positive.go",
				src: `package fixture

func positive(m map[string]int, key string) bool {
	if v, ok := m[key]; ok && v > 0 {
		return true
	}
	return false
}
`,
				prefix: "lcr-",
				want: `positive lcr-left 0: ok && v > 0 -> "ok"
positive lcr-right 0: ok && v > 0 -> "v > 0"
positive lcr-false 0: ok && v > 0 -> "false"
`,
			},
			{
				name: "marks a return of zero values that leaves out the only use of a name runnable",
				file: "results.go",
				src: `package fixture

import "strings"

func size(s string) int {
	n := len(s)
	return n
}

func upper(s string) string {
	return strings.ToUpper(s)
}
`,
				prefix: string(spec.SBRZero),
				want: `size sbr-zero 0: return n -> "return 0"
upper sbr-zero 0: return strings.ToUpper(s) -> "return \"\""
`,
			},
		}
		for _, tt := range leftOut {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				r := all(t, map[string]string{tt.file: tt.src})
				expect.Equal(t, listing(r, tt.prefix), tt.want, "every mutant of the left-out use is made")
				expect.Empty(t, statuses(r), "and runs")
			})
		}

		t.Run("marks a division of an integer by a constant 0 not viable", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"scale.go": `package fixture

const off = 0

func scale(x, y int, f float64) (int, int, float64) {
	a := x * 0
	b := x * off
	c := x * y
	x *= 0
	f *= 0
	return a + b + c, x, f * 0
}
`})
			assert.Equal(t, listing(r, string(spec.AOR)), `scale aor 0: x * 0 -> "x / 0" not-viable
scale aor 1: x * off -> "x / off" not-viable
scale aor 2: x * y -> "x / y"
scale aor 3: x *= 0 -> "x /= 0" not-viable
scale aor 4: f *= 0 -> "f /= 0"
scale aor 5: a + b + c -> "a + b - c"
scale aor 6: a + b -> "a - b"
scale aor 7: f * 0 -> "f / 0"
`, "an integer division by a constant 0 is not viable")
			for _, m := range r.Mutants {
				if m.Kind != spec.AOR {
					continue
				}
				text := string(m.Site.File.Text)
				mutated := text[:m.Site.Start] + m.Replacement + text[m.Site.End:]
				fset := token.NewFileSet()
				f, err := parser.ParseFile(fset, m.Site.File.Name, mutated, 0)
				assert.NoError(t, err, "the mutated file parses")
				_, err = new(types.Config).Check("fixture", fset, []*ast.File{f}, nil)
				if m.Status != enumerate.NotViable {
					expect.NoError(t, err, "the type checker accepts "+m.Replacement)
					continue
				}
				rejection := assert.ErrorAs[types.Error](t, err, "the type checker rejects "+m.Replacement)
				expect.Equal(t, rejection.Msg, m.Reason, "and the mutant's reason is the type checker's message")
			}
		})

		t.Run("keeps a final statement that terminates its list", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"terminating.go": `package fixture

func terminating(n int, ch chan int) int {
	switch n {
	case 0:
		return 0
	case 1:
		fallthrough
	default:
		panic(n)
	}
}

func selects(ch chan int) int {
	select {
	case v := <-ch:
		return v
	}
}

func types(x any) int {
	switch x.(type) {
	case int:
		return 1
	}
	goto end
end:
	return 2
}

func loops() int {
	for {
	}
}

func blocks(n int) int {
	{
		return n
	}
}

func ifs(n int) int {
	if n > 0 {
		return 1
	} else {
		return 2
	}
}

func open(n int) int {
	switch n {
	case 0:
	}
	return n
}

func calls(f func()) {
	f()
	(f)()
}

func gotos(n int) int {
L:
	n++
	{
		goto L
	}
}

func labelled() {
	{
	L:
		for {
			break L
		}
	}
}
`})
			assert.Equal(
				t,
				listing(r, string(spec.SBRDelete)),
				`types sbr-delete 0: switch x.(type) { case int: return 1 } -> ""
open sbr-delete 0: switch n { case 0: } -> ""
calls sbr-delete 0: f() -> ""
calls sbr-delete 1: (f)() -> ""
`,
				"only the statements that are not final or not terminating are deleted",
			)
		})

		t.Run("deletes a final statement that the Go specification does not call terminating", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"final.go": `package fixture

import "fmt"

func receives(ch chan int) {
	<-ch
}

func methods(s fmt.Stringer) {
	s.String()
}

func typeswitch(x any) {
	switch x.(type) {
	case int:
	}
}

func cases(n int) {
	switch n {
	case 1:
		n++
	}
}

func loop(ch chan int) {
	for {
		if <-ch == 0 {
			break
		}
	}
}

func classify(n int) {
	switch {
	case n > 0:
		panic("positive")
	}
}

func fail(err error) {
	if err != nil {
		(panic(err))
	}
}

func drain(ch chan int) int {
	for {
		select {
		case v := <-ch:
			if v < 0 {
				break
			}
			return v
		}
	}
}

func block() int {
	{
		return 1
		;
	}
}

func empty() {
	{
	}
}
`})
			assert.Equal(t, listing(r, string(spec.SBRDelete)), `receives sbr-delete 0: <-ch -> ""
methods sbr-delete 0: s.String() -> ""
typeswitch sbr-delete 0: switch x.(type) { case int: } -> ""
cases sbr-delete 0: switch n { case 1: n++ } -> ""
loop sbr-delete 0: for { if <-ch == 0 { break } } -> ""
loop sbr-delete 1: if <-ch == 0 { break } -> ""
classify sbr-delete 0: switch { case n > 0: panic("positive") } -> ""
fail sbr-delete 0: if err != nil { (panic(err)) } -> ""
drain sbr-delete 0: select { case v := <-ch: if v < 0 { break } return v } -> ""
drain sbr-delete 1: if v < 0 { break } -> ""
empty sbr-delete 0: { } -> ""
`, "a final loop with a break, a switch without a default and a select with a break are deleted")
		})

		t.Run("compares values of every type for equality", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"eq.go": `package fixture

func eq(i any, xs []int, p *int, f func()) bool {
	return i == 3 && xs != nil && p == nil && f != nil
}
`})
			expect.Equal(t, listing(r, "ror-"), `eq ror-true 0: i == 3 -> "true"
eq ror-false 0: i == 3 -> "false"
eq ror-true 1: xs != nil -> "true"
eq ror-false 1: xs != nil -> "false"
eq ror-true 2: p == nil -> "true"
eq ror-false 2: p == nil -> "false"
eq ror-true 3: f != nil -> "true"
eq ror-false 3: f != nil -> "false"
`, "every equality has its two mutants")
			expect.Empty(t, r.Skipped, "and no equality is skipped")
		})

		t.Run("negates no operand of a connector that is a site", func(t *testing.T) {
			t.Parallel()
			src := `package fixture

type flag bool

func operands(a, b, c bool, d flag) bool {
	_ = (a) && !b
	_ = d && flag(c)
	return a || f(c)
}

func f(c bool) bool { return c }
`
			r := all(t, map[string]string{"operands.go": src})
			expect.Equal(t, listing(r, string(spec.UOINot)), `operands uoi-not 0: c -> "!c"
operands uoi-not 1: c -> "!c"
f uoi-not 0: c -> "!c"
`, "only the operands of a skipped connector and of calls are negated")
			expect.Equal(
				t,
				skipped(r, src),
				"d && flag(c): named boolean result\n",
				"and the connector of a named boolean is skipped",
			)
		})

		t.Run("treats an alias of bool as bool", func(t *testing.T) {
			t.Parallel()
			r := all(t, map[string]string{"alias.go": `package fixture

type truth = bool

func alias(a, b int) truth {
	return a < b
}
`})
			expect.Empty(t, r.Skipped, "the comparison is not skipped")
			expect.Contains(t, listing(r, string(spec.RORBoundary)), "a < b", "and has its boundary mutant")
		})
	})
}
