// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
)

// fixture writes files into a new directory, with a go.mod of the module
// fixture at go 1.21 unless files has one, and loads and enumerates the
// package there.
func fixture(t *testing.T, files map[string]string) (*load.Package, *enumerate.Result) {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module fixture\n\ngo 1.21\n"
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := load.Load(context.Background(), load.Config{Dir: dir, Env: os.Environ(), Imports: render.Imports()})
	if err != nil {
		t.Fatal(err)
	}
	return p, enumerate.Enumerate(p, spec.Load(), nil)
}

// build renders the package, builds its test binary from the program as
// the engine builds it, and returns the binary's path.
func build(t *testing.T, p *load.Package, r *enumerate.Result) (*render.Program, string) {
	t.Helper()
	prog, err := render.Render(p, r)
	if err != nil {
		t.Fatal(err)
	}
	return prog, buildProgram(t, p, prog)
}

// buildProgram builds the package's test binary with the files of prog in
// place of the package's own, as the engine builds it, and returns the
// binary's path.
func buildProgram(t *testing.T, p *load.Package, prog *render.Program) string {
	t.Helper()
	work := t.TempDir()
	overlay, err := prog.Write(work)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(work, "pkg.test")
	if out, err := load.Go(
		context.Background(),
		p.Dir,
		os.Environ(),
		"test",
		"-c",
		"-vet=off",
		"-o",
		bin,
		"-overlay",
		overlay,
		".",
	); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
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
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", bin, env, err, out)
	}
	return string(out)
}

// ids names each mutant by its scope, its kind, and its position among the
// mutants of its scope and kind.
func ids(r *enumerate.Result) map[*enumerate.Mutant]string {
	out := map[*enumerate.Mutant]string{}
	seen := map[string]int{}
	for _, m := range r.Mutants {
		key := m.Site.Scope + " " + m.Kind
		out[m] = key + " " + strconv.Itoa(seen[key])
		seen[key]++
	}
	return out
}

// lines returns the lines of out that start with a capital letter, the
// lines that the fixture's test prints.
func lines(out string) map[string]string {
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if name, value, ok := strings.Cut(line, " "); ok && name != "" && name[0] >= 'A' && name[0] <= 'Z' {
			got[name] = value
		}
	}
	return got
}

// describe writes the value of each function of the semantics fixture.
func describe(values map[string]string) string {
	var b strings.Builder
	for _, name := range []string{
		"Add", "Later", "Mod", "Less", "AtLeast", "Same", "Both", "Either", "OrderAnd", "OrderOr", "Neg", "Not", "Inc",
		"Grow", "Steps", "Bump", "Recovers", "Keep",
	} {
		fmt.Fprintf(&b, "%s=%s ", name, values[name])
	}
	return b.String()
}

// expected returns the values of the semantics fixture's functions with m
// active: the original values, and m's value for the function of its site.
func expected(names map[*enumerate.Mutant]string, m *enumerate.Mutant) map[string]string {
	out := map[string]string{}
	for name, value := range semanticsOriginal {
		out[name] = value
	}
	out[m.Site.Scope] = semanticsWant[names[m]]
	return out
}
