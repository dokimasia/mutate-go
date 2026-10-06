// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// hiding is a file of the package fixture that declares a variable named
// hiddenName, a predeclared name that the instrumentation's helper file
// uses. The declaration hides the predeclared name in the helper file too.
const (
	hiddenName = "nil"
	hiding     = "package fixture\n\nvar " + hiddenName + " = 0\n"
)

// claiming and claimingTest are a file and a test file of the package
// fixture that declare names that the instrumentation declares in a package
// without such names.
const (
	claiming     = "package fixture\n\nvar _mutateActive, _mutateIs = 0, 0\n"
	claimingTest = "package fixture\n\nfunc _mutateHit(int) {}\n"
)

// instrumentedOverlay is the shell pattern of the overlay argument of the
// instrumented build.
const instrumentedOverlay = "*/src/overlay.json"

// instrumentedReason is the pin of the reason of a mutant that does not run
// because the instrumented build failed.
const instrumentedReason = "the instrumented build failed"

func TestBuild(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("states the build error of a test file that does not compile", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(arithFiles, map[string]string{arithTestFile: arithTest + "\n" + brokenDecl}))
			rec := runIn(t, dir, run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorBuild}, "the tests do not build")
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
			assert.That(t, rec.Errors[0].Message).
				HasPrefix("the tests do not build from the unchanged source: ",
					"the message states that the engine is not the cause").
				Contains(arithTestFile, "and names the file that does not compile")
			assert.Equal(t, rec.Mutants[0].Reason, "the tests do not build", "the record states why no mutant runs")
		})

		t.Run("states the build error of an instrumented build that fails while the unchanged source builds",
			func(t *testing.T) {
				t.Parallel()
				// The test file hides a predeclared name of the helper file in
				// the test binary, which the instrumentation does not
				// type-check.
				dir := module(t, with(arithFiles, map[string]string{"hiding_test.go": hiding}))
				rec := runIn(t, dir, run.Config{})
				assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorBuild}, "the instrumented build fails")
				assert.That(t, rec.Errors[0].Message).
					HasPrefix("the instrumented build fails, and the unchanged source builds: ",
						"the message states that the unchanged source builds").
					Contains(hiddenName, "and names the hidden name")
				assert.Equal(t, rec.Mutants[0].Reason, instrumentedReason, "the record states why no mutant runs")
			},
		)

		t.Run(
			"states the build error of a package that hides a predeclared name of the helper file",
			func(t *testing.T) {
				t.Parallel()
				dir := module(t, with(arithFiles, map[string]string{"hiding.go": hiding}))
				rec := runIn(t, dir, run.Config{})
				assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorBuild}, "the instrumentation fails")
				assert.That(t, rec.Errors[0].Message).
					HasPrefix("render: the type checker rejects the instrumented package: ",
						"the message states the type checker's verdict").
					Contains(hiddenName, "and names the hidden name")
				assert.Equal(t, rec.Mutants[0].Reason, instrumentedReason, "the record states why no mutant runs")
			},
		)

		t.Run("runs a package whose files and tests declare the names of the instrumentation", func(t *testing.T) {
			t.Parallel()
			dir := module(
				t,
				with(arithFiles, map[string]string{"claiming.go": claiming, "claiming_test.go": claimingTest}),
			)
			rec := runIn(t, dir, run.Config{})
			assert.Empty(t, rec.Errors, "the instrumented build builds")
			assert.Equal(t, verdicts(rec), arithVerdicts, "every mutant has the verdict of the package without them")
		})

		t.Run("states the build error of a package of the suite whose tests do not compile", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				conformanceTestFile: scale[conformanceTestFile] + "\n" + brokenDecl,
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorBuild}, "the suite does not build")
			assert.Equal(t, verdicts(rec), "Scale sbr-zero 0: not-run\nScale aor 0: not-run\n", "no mutant runs")
			assert.HasPrefix(t, rec.Errors[0].Message,
				"the tests of fixture/conformance do not build from the unchanged source: ",
				"the message names the package whose tests do not build")
		})

		t.Run("stops a run whose instrumented build the caller cancels", func(t *testing.T) {
			t.Parallel()
			env, marker := blocking(t, instrumentedOverlay)
			rec := cancelAt(t, module(t, arithFiles), run.Config{Env: env}, marker)
			assert.Equal(t, verdicts(rec), arithNotRun, "no mutant runs")
			assert.Equal(t, rec.Mutants[0].Reason, cancelledReason, "the record states that the caller ended the run")
			assert.Empty(t, rec.Errors, "a cancelled build states no run error")
		})
	})
}
