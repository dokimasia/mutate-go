// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
)

// otherVar and otherTraceVar are the variables of a protocol that the tests
// pass to Render in place of the protocol's.
const (
	otherVar      = "FIXTURE_MUTANT"
	otherTraceVar = "FIXTURE_TRACE"
)

// other is a protocol whose variables differ from the protocol's.
var other = spec.Protocol{Variable: otherVar, Trace: otherTraceVar}

// used is a package with one function of two mutants, and a test that
// prints its value.
var used = map[string]string{
	"f.go": "package fixture\n\nfunc Used(x int) int { return x + 1 }\n",
	"f_test.go": "package fixture\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\n" +
		"func TestUsed(t *testing.T) { fmt.Println(\"Used\", Used(1)) }\n",
}

func TestHelper(t *testing.T) {
	t.Parallel()

	t.Run("Render", func(t *testing.T) {
		t.Parallel()

		t.Run("reads the active mutant from the variable that the caller names", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			prog, err := render.Render(p, r, &other)
			assert.NoError(t, err, "the package renders with another variable")
			bin := buildProgram(t, p, prog)
			expect.Equal(t, lines(run(t, bin, p.Dir, otherVar+"=2"))["Used"], "0",
				"the variable activates the aor mutant, 1 - 1")
			expect.Equal(t, lines(run(t, bin, p.Dir, active(2)))["Used"], "2",
				"and the protocol's variable activates nothing")
		})

		t.Run("writes the trace to the file that the caller's trace variable names", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			prog, err := render.Render(p, r, &other)
			assert.NoError(t, err, "the package renders with another trace variable")
			bin := buildProgram(t, p, prog)
			dir := t.TempDir()
			ours, theirs := filepath.Join(dir, "ours"), filepath.Join(dir, "theirs")
			run(t, bin, p.Dir, otherTraceVar+"="+ours, traceVar+"="+theirs)
			files.HasContent(t, ours, startLine+"1\n2\n", "the binary traces Used to the file of the caller's variable")
			files.Absent(t, theirs, "and not to the file of the protocol's variable")
		})

		t.Run("panics in the binary when the trace does not open", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			_, bin := build(t, p, r)
			trace := filepath.Join(t.TempDir(), "missing", "trace")
			out, err := commandIn(t.Context(), bin, p.Dir, traceVar+"="+trace).CombinedOutput()
			assert.HasError(t, err, "the binary fails")
			assert.Contains(t, string(out), "panic: mutate: open", "with the error of the trace")
		})
	})

	t.Run("Imports", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the packages that the helper file imports", func(t *testing.T) {
			t.Parallel()
			assert.Permutation(t, render.Imports(), []string{"os", "strconv", "sync/atomic"},
				"the helper file imports os, strconv and sync/atomic")
		})
	})
}
