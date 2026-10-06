// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import (
	"fmt"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/enumerate"
)

// problems writes one line per problem: its code and its message.
func problems(r *enumerate.Result) string {
	var b strings.Builder
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "%s %s\n", p.Code, p.Message)
	}
	return b.String()
}

func TestSuppress(t *testing.T) {
	t.Parallel()
	t.Run("Enumerate", func(t *testing.T) {
		t.Parallel()
		t.Run("suppresses the calls and arguments of the rule families", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"rules.go": `package fixture

import (
	"log"
	"strings"
	"time"
)

func logs(n int) int {
	defer log.Println("done")
	log.Printf("n=%d", n+1)
	return n
}

func waits(t *time.Timer, d time.Duration) bool {
	time.Sleep(d * 2)
	return t.Reset(d)
}

func sizes(n int) ([]int, map[int]int, string) {
	s := make([]int, n-1, n*2)
	m := make(map[int]int, n+1)
	var b strings.Builder
	b.Grow(n * 3)
	return s, m, b.String()
}
`})
			want(t, listing(r, ""), `logs sbr-delete 0: defer log.Println("done") -> "" [logging]
logs sbr-delete 1: log.Printf("n=%d", n+1) -> "" [logging]
logs aor 0: n+1 -> "n-1" [logging]
logs sbr-zero 0: return n -> "return 0"
waits sbr-delete 0: time.Sleep(d * 2) -> "" [timing]
waits aor 0: d * 2 -> "d / 2" [timing]
waits sbr-zero 0: return t.Reset(d) -> "return false"
waits uoi-not 0: t.Reset(d) -> "!t.Reset(d)" [timing]
sizes aor 0: n-1 -> "n+1"
sizes aor 1: n*2 -> "n/2" [capacity]
sizes aor 2: n+1 -> "n-1" [capacity]
sizes sbr-delete 0: b.Grow(n * 3) -> ""
sizes aor 3: n * 3 -> "n / 3" [capacity]
sizes sbr-zero 0: return s, m, b.String() -> "return nil, nil, \"\""
`)
		})
		t.Run("suppresses the marks of test helpers by their APIs and by the method rule", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"helpers.go": `package fixture

import "testing"

// tester is a library's own interface with the method of testing.TB.
type tester interface {
	Helper()
	Errorf(format string, args ...any)
}

// marker has a method Helper of its own, which marks no test helper.
type marker interface {
	Helper(depth int)
}

type concrete struct{}

func (concrete) Helper() {}

func viaTB(tb testing.TB) {
	tb.Helper()
}

func viaT(t *testing.T) {
	t.Helper()
}

func viaF(f *testing.F) {
	f.Helper()
}

func viaOwn(tb tester) {
	tb.Helper()
}

func viaConstraint[T interface{ Helper() }](tb T) {
	tb.Helper()
}

func viaOther(m marker, c concrete) {
	m.Helper(1)
	c.Helper()
}
`})
			want(t, listing(r, "sbr-delete"), `viaTB sbr-delete 0: tb.Helper() -> "" [helper]
viaT sbr-delete 0: t.Helper() -> "" [helper]
viaF sbr-delete 0: f.Helper() -> "" [helper]
viaOwn sbr-delete 0: tb.Helper() -> "" [helper]
viaConstraint sbr-delete 0: tb.Helper() -> "" [helper]
viaOther sbr-delete 0: m.Helper(1) -> ""
viaOther sbr-delete 1: c.Helper() -> ""
`)
		})
		t.Run(
			"suppresses the calls of a variable whose every value is the cancel function of a deadline",
			func(t *testing.T) {
				t.Parallel()
				r := enumerateFixture(t, map[string]string{"cancel.go": `package fixture

import (
	"context"
	"time"
)

var global context.CancelFunc

type box struct{ n int }

func stop(f *context.CancelFunc) { (*f)() }

func declared(parent context.Context) context.Context {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return ctx
}

func valued(parent context.Context) context.Context {
	var ctx, cancel = context.WithTimeout(parent, time.Second)
	defer cancel()
	return ctx
}

func assigned(parent context.Context, d time.Time) {
	var cancel context.CancelFunc
	parent, cancel = context.WithDeadline(parent, d)
	cancel()
	go cancel()
	defer func() {
		cancel()
	}()
	if cancel != nil {
		cancel()
	}
	_ = parent
}

func untyped(parent context.Context) {
	var f func()
	_, f = context.WithTimeout(parent, time.Second)
	f()
}

func cancelled(parent context.Context) {
	_, cancel := context.WithCancel(parent)
	defer cancel()
}

func mixed(parent context.Context, bound bool) {
	cancel := context.CancelFunc(func() {})
	if bound {
		_, cancel = context.WithTimeout(parent, time.Second)
	}
	cancel()
}

func addressed(parent context.Context) {
	_, cancel := context.WithTimeout(parent, time.Second)
	stop(&cancel)
	cancel()
}

func parameter(cancel context.CancelFunc) {
	cancel()
}

func named() (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	cancel()
	return
}

func shared(parent context.Context) {
	_, global = context.WithTimeout(parent, time.Second)
	global()
}

func other(b *box, m map[string]int, xs []int) int {
	b.n = 1
	v, ok := m["k"]
	for i := range xs {
		v += i
	}
	if ok {
		return v
	}
	return 0
}
`})
				want(t, listing(r, "sbr-delete"), `stop sbr-delete 0: (*f)() -> ""
declared sbr-delete 0: defer cancel() -> "" [timing]
valued sbr-delete 0: defer cancel() -> "" [timing]
assigned sbr-delete 0: parent, cancel = context.WithDeadline(parent, d) -> ""
assigned sbr-delete 1: cancel() -> "" [timing]
assigned sbr-delete 2: go cancel() -> "" [timing]
assigned sbr-delete 3: defer func() { cancel() }() -> ""
assigned sbr-delete 4: cancel() -> "" [timing]
assigned sbr-delete 5: if cancel != nil { cancel() } -> "" [timing]
assigned sbr-delete 6: cancel() -> "" [timing]
untyped sbr-delete 0: _, f = context.WithTimeout(parent, time.Second) -> ""
untyped sbr-delete 1: f() -> "" [timing]
cancelled sbr-delete 0: defer cancel() -> ""
mixed sbr-delete 0: if bound { _, cancel = context.WithTimeout(parent, time.Second) } -> ""
mixed sbr-delete 1: _, cancel = context.WithTimeout(parent, time.Second) -> ""
mixed sbr-delete 2: cancel() -> ""
addressed sbr-delete 0: stop(&cancel) -> ""
addressed sbr-delete 1: cancel() -> ""
parameter sbr-delete 0: cancel() -> ""
named sbr-delete 0: ctx, cancel = context.WithTimeout(context.Background(), time.Second) -> ""
named sbr-delete 1: cancel() -> ""
shared sbr-delete 0: _, global = context.WithTimeout(parent, time.Second) -> ""
shared sbr-delete 1: global() -> ""
other sbr-delete 0: b.n = 1 -> ""
other sbr-delete 1: for i := range xs { v += i } -> ""
other sbr-delete 2: v += i -> ""
other sbr-delete 3: if ok { return v } -> ""
`)
			},
		)
		t.Run("states the rule and no reason of a not-viable mutant that a rule family suppresses", func(t *testing.T) {
			t.Parallel()
			// The aor mutant of n * 0 divides an integer by a constant 0, which
			// the compiler rejects.
			r := enumerateFixture(t, map[string]string{"logs.go": `package fixture

import "log"

func logs(n int) {
	log.Println(n * 0)
}
`})
			for _, m := range r.Mutants {
				if m.Kind == enumerate.AOR &&
					(m.Status != enumerate.Suppressed || m.Rule != "logging" || m.Reason != "") {
					t.Errorf(
						"aor has status %d, rule %q and reason %q, want suppressed by logging",
						m.Status,
						m.Rule,
						m.Reason,
					)
				}
			}
		})
		t.Run("resolves the API of a call through embedding, instantiation and go statements", func(t *testing.T) {
			t.Parallel()
			calls := `package fixture

import (
	"log"
	"slices"
)

type service struct{ *log.Logger }

const a = 1 + 1

func calls(s service, xs []int, n int) []int {
	s.Printf("%d", n+1)
	go log.Println(n - 1)
	ch := make(chan int, n+2)
	close(ch)
	n = n * 2
	xs = slices.Grow[[]int, int](xs, n*3)
	return slices.Grow[[]int](xs, n*2)
}
`
			other := "package fixture\n\nconst b = 2 * 2\n\nfunc other(n int) int { return n - 1 }\n"
			r := enumerateFixture(t, map[string]string{"calls.go": calls, "other.go": other})
			want(t, listing(r, "aor"), `calls aor 0: n+1 -> "n-1" [logging]
calls aor 1: n - 1 -> "n + 1" [logging]
calls aor 2: n+2 -> "n-2"
calls aor 3: n * 2 -> "n / 2"
calls aor 4: n*3 -> "n/3" [capacity]
calls aor 5: n*2 -> "n/2" [capacity]
other aor 0: n - 1 -> "n + 1"
`)
			want(t, listing(r, "sbr-delete"), `calls sbr-delete 0: s.Printf("%d", n+1) -> "" [logging]
calls sbr-delete 1: go log.Println(n - 1) -> "" [logging]
calls sbr-delete 2: close(ch) -> ""
calls sbr-delete 3: n = n * 2 -> ""
calls sbr-delete 4: xs = slices.Grow[[]int, int](xs, n*3) -> ""
`)
			var files []string
			for _, s := range r.Skipped {
				files = append(files, s.File)
			}
			want(t, strings.Join(files, " "), "calls.go other.go")
		})
		t.Run("suppresses the annotated kinds on one line", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"notes.go": `package fixture

func best(xs []int) int {
	top := 0
	for _, v := range xs {
		if v > top { //dokimi:mutate-skip ror-boundary: an equal value leaves top unchanged
			top = v
		}
	}
	return top
}

func clamp(x int) int {
	//dokimi:mutate-skip ror: the caller checks the bound
	if x > 10 {
		return 10
	}
	return x
}
`})
			var b strings.Builder
			n := nth(r)
			for _, m := range r.Mutants {
				if m.Status == enumerate.Suppressed {
					fmt.Fprintf(&b, "%s %s %d: %s\n", m.Site.Scope, m.Kind, n[m], m.Reason)
				}
			}
			want(t, b.String(), `best ror-boundary 0: an equal value leaves top unchanged
clamp ror-boundary 0: the caller checks the bound
clamp ror-false 0: the caller checks the bound
`)
			if len(r.Problems) != 0 {
				t.Errorf("problems %v, want none", r.Problems)
			}
		})
		t.Run("reports an annotation without a reason or without a mutant", func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				name, give, want string
			}{
				{"no reason", "if x > 1 { //dokimi:mutate-skip ror-boundary", "annotation-without-reason"},
				{"empty reason", "if x > 1 { //dokimi:mutate-skip ror-boundary:  ", "annotation-without-reason"},
				{"stale", "if x > 1 { //dokimi:mutate-skip aor: no arithmetic here", "stale-annotation"},
				{"unknown kind", "if x > 1 { //dokimi:mutate-skip ror-bound: a misspelt kind", "stale-annotation"},
				{"another directive", "if x > 1 { //dokimi:mutate-skipped ror-boundary: another directive", ""},
				{"two kinds", "if x > 1 { //dokimi:mutate-skip ror-boundary, sbr: two kinds", ""},
				{"every kind", "if x > 1 { //dokimi:mutate-skip all: every kind", ""},
				{"next line", "//dokimi:mutate-skip ror-boundary: on the line below\n\tif x > 1 {", ""},
				{
					"two lines above",
					"//dokimi:mutate-skip ror-boundary: two lines below\n\n\tif x > 1 {",
					"stale-annotation",
				},
			}
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					src := "package fixture\n\nfunc f(x int) int {\n\t" + tt.give + "\n\t\treturn 1\n\t}\n\treturn x\n}\n"
					r := enumerateFixture(t, map[string]string{"f.go": src})
					var codes []string
					for _, p := range r.Problems {
						codes = append(codes, p.Code)
					}
					if got := strings.Join(codes, " "); got != tt.want {
						t.Errorf("problem codes %q, want %q", got, tt.want)
					}
				})
			}
		})
		t.Run("names the annotation's file and line in a problem", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"f.go": `package fixture

func f(x int) int {
	if x > 1 { //dokimi:mutate-skip ror-boundary
		return 1
	}
	//dokimi:mutate-skip aor: no arithmetic below
	return x
}
`})
			want(t, problems(r), `annotation-without-reason f.go:4: the annotation states no reason
stale-annotation f.go:7: the annotation suppresses no mutant
`)
		})
	})
}
