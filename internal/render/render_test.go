// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/render"
)

const semantics = `package fixture

import (
	"strings"
	"time"
)

func Add(a, b int) int { return a + b }

func Later(a, b time.Duration) time.Duration { return a + b }

func Mod(a, b int) int { return a % b }

func Less(a, b float64) bool { return a < b }

func AtLeast(a, b int) bool { return a >= b }

func Same(a any, b int) bool { return a == b }

func Both(a, b bool) bool { return a && b }

func Either(a, b bool) bool { return a || b }

func OrderAnd(a, b bool) string { return probe(seen("a", a) && seen("b", b)) }

func OrderOr(a, b bool) string { return probe(seen("a", a) || seen("b", b)) }

func Neg(x int) int { return -x }

func Not(x bool) bool { return !x }

func Inc(x int) int {
	x++
	return x
}

func Grow(x uint8) uint8 {
	x += 200
	return x
}

func Steps(n int) int {
	c := 0
	for i := 1; i*i < 9; i++ { //dokimi:mutate-skip aor: the loop would not end
		c += n
	}
	return c
}

func Bump(xs []int, a, b int) []int {
	xs[a+b]++
	return xs
}

func Recovers(fail bool) (caught bool) {
	defer func() {
		caught = recover() != nil && fail
	}()
	if fail {
		panic("fail")
	}
	return false
}

func Keep(s string, ok bool) string {
	if v := len(s); ok && v > 1 {
		return strings.ToUpper(s)
	}
	return s + "."
}
`

const semanticsTest = `package fixture

import (
	"fmt"
	"testing"
	"time"
)

func TestPrint(t *testing.T) {
	fmt.Println("Add", Add(5, 3))
	fmt.Println("Later", Later(2*time.Second, 3*time.Second))
	fmt.Println("Mod", Mod(7, 3))
	fmt.Println("Less", Less(2, 2))
	fmt.Println("AtLeast", AtLeast(2, 2))
	fmt.Println("Same", Same(2, 2))
	fmt.Println("Both", Both(true, false))
	fmt.Println("Either", Either(false, true))
	fmt.Println("OrderAnd", OrderAnd(true, false))
	fmt.Println("OrderOr", OrderOr(false, true))
	fmt.Println("Neg", Neg(4))
	fmt.Println("Not", Not(true))
	fmt.Println("Inc", Inc(4))
	fmt.Println("Grow", Grow(100))
	fmt.Println("Steps", Steps(5))
	fmt.Println("Bump", Bump([]int{0, 0, 0, 0}, 1, 1))
	fmt.Println("Recovers", guarded(Recovers, true))
	fmt.Println("Keep", Keep("ab", true))
}
`

// semanticsProbe records the operands that a connector evaluates, in the
// order of evaluation, and states a panic that a function passes on. It is
// generated, so it has no site of its own.
const semanticsProbe = `// Code generated for the test. DO NOT EDIT.

package fixture

import "strconv"

var trail string

func seen(name string, v bool) bool {
	trail += name
	return v
}

func probe(r bool) string {
	s := strconv.FormatBool(r) + ":"
	s, trail = s+trail, ""
	return s
}

func guarded(f func(bool) bool, v bool) (s string) {
	defer func() {
		if recover() != nil {
			s = "panicked"
		}
	}()
	return strconv.FormatBool(f(v))
}
`

// semanticsOriginal is the value of each function of the semantics fixture
// with no mutant active. OrderAnd and OrderOr state the result, a colon,
// and the operands that the connector evaluated, in order.
var semanticsOriginal = map[string]string{
	"Add":      "8",
	"Later":    "5s",
	"Mod":      "1",
	"Less":     "false",
	"AtLeast":  "true",
	"Same":     "true",
	"Both":     "false",
	"Either":   "true",
	"OrderAnd": "false:ab",
	"OrderOr":  "true:ab",
	"Neg":      "-4",
	"Not":      "false",
	"Inc":      "5",
	"Grow":     "44",
	"Steps":    "10",
	"Bump":     "[0 0 1 0]",
	"Recovers": "true",
	"Keep":     "AB",
}

// semanticsWant is the value of the function of each runnable mutant of the
// semantics fixture with the mutant active, by the mutant's name.
var semanticsWant = map[string]string{
	"Add sbr-zero 0":         "0",
	"Add aor 0":              "2",
	"Later sbr-zero 0":       "0s",
	"Later aor 0":            "-1s",
	"Mod sbr-zero 0":         "0",
	"Mod aor 0":              "21",
	"Less sbr-zero 0":        "false",
	"Less ror-boundary 0":    "true",
	"Less ror-false 0":       "false",
	"AtLeast sbr-zero 0":     "false",
	"AtLeast ror-boundary 0": "false",
	"AtLeast ror-true 0":     "true",
	"Same sbr-zero 0":        "false",
	"Same ror-true 0":        "true",
	"Same ror-false 0":       "false",
	"Both sbr-zero 0":        "false",
	"Both lcr-left 0":        "true",
	"Both lcr-right 0":       "false",
	"Both lcr-false 0":       "false",
	"Either sbr-zero 0":      "false",
	"Either lcr-left 0":      "false",
	"Either lcr-right 0":     "true",
	"Either lcr-true 0":      "true",
	"OrderAnd sbr-zero 0":    "",
	"OrderAnd lcr-left 0":    "true:a",
	"OrderAnd lcr-right 0":   "false:b",
	"OrderAnd lcr-false 0":   "false:",
	"OrderAnd uoi-not 0":     "false:a",
	"OrderAnd uoi-not 1":     "true:ab",
	"OrderOr sbr-zero 0":     "",
	"OrderOr lcr-left 0":     "false:a",
	"OrderOr lcr-right 0":    "true:b",
	"OrderOr lcr-true 0":     "true:",
	"OrderOr uoi-not 0":      "true:a",
	"OrderOr uoi-not 1":      "false:ab",
	"Neg sbr-zero 0":         "0",
	"Neg uoi-minus 0":        "4",
	"Not sbr-zero 0":         "false",
	"Not uoi-not 0":          "true",
	"Inc uoi-incdec 0":       "3",
	"Inc sbr-zero 0":         "0",
	"Grow aor 0":             "156",
	"Grow sbr-delete 0":      "100",
	"Grow sbr-zero 0":        "0",
	"Steps sbr-delete 0":     "0",
	"Steps ror-boundary 0":   "15",
	"Steps ror-false 0":      "0",
	"Steps uoi-incdec 0":     "20",
	"Steps aor 1":            "-10",
	"Steps sbr-delete 1":     "0",
	"Steps sbr-zero 0":       "0",
	"Bump uoi-incdec 0":      "[0 0 -1 0]",
	"Bump aor 0":             "[1 0 0 0]",
	"Bump sbr-zero 0":        "[]",
	// recover in an operand of the connector recovers the panic wherever the
	// mutant evaluates the operand.
	"Recovers sbr-delete 0": "panicked",
	"Recovers sbr-delete 1": "panicked",
	"Recovers lcr-left 0":   "true",
	"Recovers lcr-right 0":  "panicked",
	"Recovers lcr-false 0":  "panicked",
	"Recovers ror-true 0":   "true",
	"Recovers ror-false 0":  "false",
	"Recovers sbr-delete 2": "false",
	"Recovers uoi-not 0":    "false",
	// The deletion, the first zero return, lcr-left and lcr-false leave out
	// the only use of v or of the package strings.
	"Keep sbr-delete 0":   "ab.",
	"Keep lcr-left 0":     "AB",
	"Keep lcr-right 0":    "AB",
	"Keep lcr-false 0":    "ab.",
	"Keep ror-boundary 0": "AB",
	"Keep ror-false 0":    "ab.",
	"Keep sbr-zero 0":     "",
	"Keep sbr-zero 1":     "AB",
}

// semanticsFiles returns the files of the semantics fixture.
func semanticsFiles() map[string]string {
	return map[string]string{
		"f.go":      semantics,
		"f_test.go": semanticsTest,
		"probe.go":  semanticsProbe,
		"types.go":  "package fixture\n\n// Pair is a type without a site.\ntype Pair struct{ A, B int }\n",
	}
}

func TestRender(t *testing.T) {
	t.Parallel()
	t.Run("Render", func(t *testing.T) {
		t.Parallel()
		t.Run("computes each mutant as the catalogue defines it when its ordinal is active", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, semanticsFiles())
			prog, bin := build(t, p, r)
			got := lines(run(t, bin, p.Dir, "DOKIMI_MUTATE_MUTANT=0"))
			if describe(got) != describe(semanticsOriginal) {
				t.Errorf("with no mutant active: %s\nwant: %s", describe(got), describe(semanticsOriginal))
			}
			names := ids(r)
			ran := map[string]bool{}
			for _, m := range r.Mutants {
				if m.Status != enumerate.Runnable {
					continue
				}
				got := lines(run(t, bin, p.Dir, "DOKIMI_MUTATE_MUTANT="+strconv.Itoa(prog.Ordinals[m])))
				if want := expected(names, m); describe(got) != describe(want) {
					t.Errorf("%s, ordinal %d: %s\nwant: %s", names[m], prog.Ordinals[m], describe(got), describe(want))
				}
				ran[names[m]] = true
			}
			for id := range semanticsWant {
				if !ran[id] {
					t.Errorf("%s did not run", id)
				}
			}
		})
		t.Run("writes each form without a line of its own", func(t *testing.T) {
			t.Parallel()
			src := "package fixture\n\nfunc f(a, b int, ok bool) int {\n\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\treturn -a\n}\n"
			p, r := fixture(t, map[string]string{"f.go": src})
			prog, err := render.Render(p, r)
			if err != nil {
				t.Fatal(err)
			}
			got := string(prog.Files[filepath.Join(p.Dir, "f.go")])
			want := "package fixture\n\nfunc f(a, b int, ok bool) int {\n" +
				"\tif !_mutateIs(1) { if (!_mutateCT(2, 4) && (_mutateActive == 3 || (_mutate_s5[int](a, b))) && \n" +
				"(_mutateActive == 2 || (ok))) {\n" +
				"\t\tif _mutateIs(7) { a-- } else { a++ }\n" +
				"\t} }\n" +
				"\tif _mutateIs(8) { return 0 }; return _mutate_s9[int](a)\n}\n"
			if got != want {
				t.Errorf("instrumented file:\n%s\nwant:\n%s", got, want)
			}
		})
		t.Run("numbers the mutants of the instrumented sites consecutively in source order", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(
				t,
				map[string]string{
					"f.go": "package fixture\n\nfunc f(a, b int) bool {\n\treturn a < b //dokimi:mutate-skip ror-false: a test of the numbering\n}\n\nfunc g(a int) int {\n\t//dokimi:mutate-skip sbr-zero: a test of the numbering\n\treturn a * 2\n}\n",
				},
			)
			prog, err := render.Render(p, r)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for _, m := range r.Mutants {
				b.WriteString(m.Kind + " " + strconv.Itoa(prog.Ordinals[m]) + "\n")
			}
			want := "sbr-zero 1\nror-boundary 2\nror-false 3\nsbr-zero 0\naor 4\n"
			if b.String() != want {
				t.Errorf("ordinals:\n%s\nwant:\n%s", b.String(), want)
			}
			if len(prog.Sites) != 3 {
				t.Errorf("Sites lists %d sites, want the 3 with a runnable mutant", len(prog.Sites))
			}
		})
		t.Run("traces the start and each site that executes once", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":      "package fixture\n\nfunc Used(x int) int { return x + 1 }\n\nfunc Unused(x int) int { return x - 1 }\n",
				"f_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestUsed(t *testing.T) {\n\tUsed(1)\n\tUsed(2)\n}\n",
			})
			_, bin := build(t, p, r)
			trace := filepath.Join(t.TempDir(), "trace")
			run(t, bin, p.Dir, "DOKIMI_MUTATE_MUTANT=0", "DOKIMI_MUTATE_TRACE="+trace)
			data, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(data); got != "start\n1\n2\n" {
				t.Errorf("trace %q, want the start and the two sites of Used", got)
			}
		})
		t.Run("panics in the binary when the trace does not open", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":      "package fixture\n\nfunc Used(x int) int { return x + 1 }\n",
				"f_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestUsed(t *testing.T) { Used(1) }\n",
			})
			_, bin := build(t, p, r)
			cmd := commandIn(bin, p.Dir, "DOKIMI_MUTATE_TRACE="+filepath.Join(t.TempDir(), "missing", "trace"))
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "panic: mutate: open") {
				t.Errorf("the binary ran with an unwritable trace: %v\n%s", err, out)
			}
		})
		t.Run("raises the language version of a module below go 1.18 and keeps every line", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"go.mod":    "module fixture\n\ngo 1.16\n",
				"a.go":      "//go:build go1.17 && !nonexistent\n// +build go1.17,!nonexistent\n\npackage fixture\n\nfunc A(x int) int { return x + 1 }\n",
				"b.go":      "// Copyright notice.\n\n//go:build go1.20\n\npackage fixture\n\nfunc B(x int) int { return x * 2 }\n",
				"c.go":      "package fixture\n\n// C returns x minus one.\nfunc C(x int) int { return x - 1 }\n",
				"f_test.go": "package fixture\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestPrint(t *testing.T) { fmt.Println(\"Sum\", A(1)+B(2)+C(3)) }\n",
			})
			prog, bin := build(t, p, r)
			a := string(prog.Files[filepath.Join(p.Dir, "a.go")])
			b := string(prog.Files[filepath.Join(p.Dir, "b.go")])
			blank := func(line string) string { return strings.Repeat(" ", len(line)) }
			wantA := "//go:build go1.18\n//line " + filepath.Join(p.Dir, "a.go") + ":1:1\n" +
				blank(
					"//go:build go1.17 && !nonexistent",
				) + "\n" + blank("// +build go1.17,!nonexistent") + "\n\npackage fixture\n"
			wantB := "//go:build go1.20\n//line " + filepath.Join(
				p.Dir,
				"b.go",
			) + ":1:1\n// Copyright notice.\n\n" + blank(
				"//go:build go1.20",
			) + "\n\npackage fixture\n"
			if !strings.HasPrefix(a, wantA) || !strings.HasPrefix(b, wantB) {
				t.Errorf("a.go:\n%s\nb.go:\n%s\nwant prefixes:\n%s\n%s", a, b, wantA, wantB)
			}
			if helper := string(
				prog.Files[filepath.Join(p.Dir, "zz_mutate.go")],
			); !strings.HasPrefix(
				helper,
				"//go:build go1.18\n\npackage fixture\n",
			) {
				t.Errorf("the helper file starts %q, want a go1.18 build constraint", helper[:40])
			}
			if got := lines(run(t, bin, p.Dir, "DOKIMI_MUTATE_MUTANT=0"))["Sum"]; got != "8" {
				t.Errorf("Sum = %s, want 8", got)
			}
		})
		t.Run("names the helper file after the names that the package directory uses", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":         "package fixture\n\nfunc F(x int) int { return x + 1 }\n",
				"zz_mutate.go": "//go:build ignore\n\npackage fixture\n",
			})
			prog, err := render.Render(p, r)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := prog.Files[filepath.Join(p.Dir, "zz_mutate_1.go")]; !ok {
				t.Errorf("files %v, want the helper file zz_mutate_1.go", keys(prog.Files))
			}
		})
		t.Run("marks the mutants of a site whose form the type checker rejects not viable", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(
				t,
				map[string]string{
					"f.go": "package fixture\n\nfunc F(a, b int) bool { return a < b }\n\nfunc G(x int) int { return x + 1 }\n",
				},
			)
			for _, s := range r.Sites {
				if s.Form == enumerate.Ordered {
					s.TypeArg = "string"
				}
			}
			prog, err := render.Render(p, r)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range r.Mutants {
				wantStatus := enumerate.Runnable
				if m.Site.Form == enumerate.Ordered {
					wantStatus = enumerate.NotViable
				}
				if m.Status != wantStatus ||
					(wantStatus == enumerate.NotViable) != (m.Reason != "" && prog.Ordinals[m] == 0) {
					t.Errorf("%s has status %d, reason %q and ordinal %d", m.Kind, m.Status, m.Reason, prog.Ordinals[m])
				}
			}
		})
		t.Run("marks the mutants of a site whose form does not parse not viable", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			for _, s := range r.Sites {
				if s.Form == enumerate.Zero {
					s.Zeros = []string{"}"}
				}
			}
			if _, err := render.Render(p, r); err != nil {
				t.Fatal(err)
			}
			for _, m := range r.Mutants {
				if (m.Kind == enumerate.SBRZero) != (m.Status == enumerate.NotViable) {
					t.Errorf("%s has status %d and reason %q", m.Kind, m.Status, m.Reason)
				}
			}
		})
		t.Run("gives up a site on its primary error and ignores the secondary lines", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			for _, s := range r.Sites {
				if s.Form == enumerate.Zero {
					s.Zeros = []string{"func() int { type x int; type x int; return 0 }()"}
				}
			}
			if _, err := render.Render(p, r); err != nil {
				t.Fatal(err)
			}
			for _, m := range r.Mutants {
				if (m.Kind == enumerate.SBRZero) != (m.Status == enumerate.NotViable && strings.Contains(m.Reason, "redeclared")) {
					t.Errorf("%s has status %d and reason %q", m.Kind, m.Status, m.Reason)
				}
			}
		})
		t.Run("returns an error when the type checker rejects the package outside every form", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			p.Name = "other"
			if _, err := render.Render(
				p,
				r,
			); err == nil ||
				!strings.HasPrefix(err.Error(), "render: the type checker rejects the instrumented package: ") {
				t.Errorf("Render() error = %v, want one that names the rejection", err)
			}
		})
		t.Run("returns an error when the package directory does not list", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			p.Dir = filepath.Join(p.Dir, "f.go")
			if _, err := render.Render(p, r); err == nil || !strings.HasPrefix(err.Error(), "render: ") {
				t.Errorf("Render() error = %v, want an error of the helper file's name", err)
			}
		})
	})
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		prog := &render.Program{Files: map[string][]byte{"/pkg/b.go": []byte("b"), "/pkg/a.go": []byte("a")}}
		t.Run("writes every file and an overlay that maps each path to its copy", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			overlay, err := prog.Write(dir)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(overlay)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"Replace":{"/pkg/a.go":"` + filepath.Join(
				dir,
				"0",
				"a.go",
			) + `","/pkg/b.go":"` + filepath.Join(
				dir,
				"1",
				"b.go",
			) + `"}}`
			if string(data) != want {
				t.Errorf("overlay %s, want %s", data, want)
			}
			if a, err := os.ReadFile(filepath.Join(dir, "0", "a.go")); err != nil || string(a) != "a" {
				t.Errorf("the copy of a.go reads %q, %v", a, err)
			}
		})
		t.Run("returns an error when a file does not write", func(t *testing.T) {
			t.Parallel()
			tests := map[string]string{"directory": "0", "file": "0/a.go", "overlay": "overlay.json"}
			for name, blocked := range tests {
				name, blocked := name, blocked
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, blocked)), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dir, blocked), 0o755); err != nil {
						t.Fatal(err)
					}
					if name == "directory" {
						if err := os.Remove(filepath.Join(dir, blocked)); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, blocked), nil, 0o644); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := prog.Write(dir); err == nil || !strings.HasPrefix(err.Error(), "render: ") {
						t.Errorf("Write() error = %v, want one that starts with render:", err)
					}
				})
			}
		})
	})
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
