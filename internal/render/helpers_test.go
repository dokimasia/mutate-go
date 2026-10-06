// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
)

// The module of a fixture: the name of its module file, and the module file
// that fixture writes where a test states none.
const (
	goMod  = "go.mod"
	module = "module fixture\n\ngo 1.21\n"
)

// The modes of the files and the directories that the tests write.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// mutantVar is the protocol's variable, which states the active mutant.
var mutantVar = spec.Load().Protocol.Variable

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

// fixture writes files into a new directory, with the module file module
// unless files has one, and loads and enumerates the package there.
func fixture(t *testing.T, files map[string]string) (*load.Package, *enumerate.Result) {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files[goMod]; !ok {
		files[goMod] = module
	}
	for name, text := range files {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), fileMode), name+" is written")
	}
	p, err := load.Load(context.Background(), load.Config{Dir: dir, Env: os.Environ(), Imports: render.Imports()})
	assert.NoError(t, err, "the fixture loads")
	return p, enumerate.Enumerate(p, spec.Load(), enumerate.Options{})
}

// instrument renders the package with the protocol's variable, and stops
// the test when Render fails.
func instrument(t *testing.T, p *load.Package, r *enumerate.Result) *render.Program {
	t.Helper()
	prog, err := render.Render(p, r, mutantVar)
	assert.NoError(t, err, "the package renders")
	return prog
}

// build renders the package, builds its test binary from the program as
// the engine builds it, and returns the program and the binary's path.
func build(t *testing.T, p *load.Package, r *enumerate.Result) (*render.Program, string) {
	t.Helper()
	prog := instrument(t, p, r)
	return prog, buildProgram(t, p, prog)
}

// buildProgram builds the package's test binary with the files of prog in
// place of the package's own, as the engine builds it, and returns the
// binary's path.
func buildProgram(t *testing.T, p *load.Package, prog *render.Program) string {
	t.Helper()
	work := t.TempDir()
	overlay, err := prog.Write(work)
	assert.NoError(t, err, "the program writes")
	bin := filepath.Join(work, "pkg.test")
	out, err := load.Go(
		context.Background(), p.Dir, os.Environ(), "test", "-c", "-vet=off", "-o", bin, "-overlay", overlay, ".",
	)
	assert.NoError(t, err, "the test binary builds: "+string(out))
	return bin
}

// commandIn returns the command that runs bin in dir, with the environment
// of the test process and the variables env.
func commandIn(bin, dir string, env ...string) *exec.Cmd {
	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd
}

// run runs the test binary in dir with the environment variables env, and
// returns its standard output.
func run(t *testing.T, bin, dir string, env ...string) string {
	t.Helper()
	out, err := commandIn(bin, dir, env...).Output()
	assert.NoError(t, err, "the test binary passes: "+string(out))
	return string(out)
}

// active returns the variable of the environment that activates the mutant
// of the ordinal.
func active(ordinal int) string {
	return mutantVar + "=" + strconv.Itoa(ordinal)
}

// ids names each mutant by its scope, its kind, and its position among the
// mutants of its scope and kind.
func ids(r *enumerate.Result) map[*enumerate.Mutant]string {
	out := map[*enumerate.Mutant]string{}
	seen := map[string]int{}
	for _, m := range r.Mutants {
		key := m.Site.Scope + " " + string(m.Kind)
		out[m] = key + " " + strconv.Itoa(seen[key])
		seen[key]++
	}
	return out
}

// lines returns the lines of out that start with a capital letter, the
// lines that the fixture's test prints, by the function they name.
func lines(out string) map[string]string {
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if name, value, ok := strings.Cut(line, " "); ok && name != "" && name[0] >= 'A' && name[0] <= 'Z' {
			got[name] = value
		}
	}
	return got
}

// describe writes the value of each function of the semantics fixture, in
// the order of their names.
func describe(values map[string]string) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(semanticsOriginal)) {
		fmt.Fprintf(&b, "%s=%s ", name, values[name])
	}
	return b.String()
}

// expected returns the values of the semantics fixture's functions with m
// active: the original values, and m's value for the function of its site.
func expected(names map[*enumerate.Mutant]string, m *enumerate.Mutant) map[string]string {
	out := maps.Clone(semanticsOriginal)
	out[m.Site.Scope] = semanticsWant[names[m]]
	return out
}
