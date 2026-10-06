// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// addLine is the line of arith's function Add.
const addLine = 5

func TestCount(t *testing.T) {
	t.Parallel()

	t.Run("Count", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the number of mutants that a run tests", func(t *testing.T) {
			t.Parallel()
			got := run.Count(t.Context(), run.Config{Dir: module(t, arithFiles), Env: os.Environ()})
			assert.Equal(t, got, 6, "arith has two mutants in each of its three functions")
		})

		t.Run("returns the number of the selected mutants", func(t *testing.T) {
			t.Parallel()
			dir := module(t, arithFiles)
			lines := []selection.Lines{{Path: filepath.Join(dir, arithFile), First: addLine, Last: addLine}}
			got := run.Count(t.Context(), run.Config{Dir: dir, Env: os.Environ(), Lines: lines})
			assert.Equal(t, got, 2, "the selection contains Add alone")
		})

		t.Run("leaves out a mutant that an annotation suppresses", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(arithFiles, map[string]string{
				arithFile: strings.Replace(arith, "func Add", fmt.Sprintf("%s %s: Sub checks it\nfunc Add",
					annotation, spec.AOR), 1),
			}))
			got := run.Count(t.Context(), run.Config{Dir: dir, Env: os.Environ()})
			assert.Equal(t, got, 5, "Add's aor mutant is suppressed")
		})

		t.Run("returns at most the sample", func(t *testing.T) {
			t.Parallel()
			got := run.Count(t.Context(), run.Config{Dir: module(t, arithFiles), Env: os.Environ(), Sample: 4})
			assert.Equal(t, got, 4, "the run tests the first four mutants alone")
		})

		t.Run("returns 0 for a package with an annotation that is not valid", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(arithFiles, map[string]string{
				arithFile: strings.Replace(arith, "func Sub", fmt.Sprintf("%s %s: nothing to skip\nfunc Sub",
					annotation, spec.RORBoundary), 1),
			}))
			got := run.Count(t.Context(), run.Config{Dir: dir, Env: os.Environ()})
			assert.Equal(t, got, 0, "the run stops at the stale annotation")
		})

		t.Run("returns 0 for a package that does not load", func(t *testing.T) {
			t.Parallel()
			got := run.Count(t.Context(), run.Config{Dir: filepath.Join(t.TempDir(), missing), Env: os.Environ()})
			assert.Equal(t, got, 0, "a directory that does not exist loads no package")
		})
	})
}
