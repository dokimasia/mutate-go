// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package mutate_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// recordDirVar is the variable of the environment that names the directory
// of Check's record, pinned.
const recordDirVar = "DOKIMI_MUTATE_RECORD_DIR"

// The variables of the environment that the tests set: the temporary
// directory and the threads of a Go program.
const (
	tmpVar   = "TMPDIR"
	procsVar = "GOMAXPROCS"
)

// The variables of the fixture's environment.
const (
	// failVar makes TestAdd fail.
	failVar = "FIXTURE_FAIL"
	// slowVar makes TestAdd sleep half a second in a run with a trace, which
	// raises the deadline of a mutant's run above 7 seconds.
	slowVar = "FIXTURE_SLOW"
	// expectedProcsVar is the GOMAXPROCS that TestAdd expects.
	expectedProcsVar = "FIXTURE_PROCS"
	// ordinaryVar makes TestSub check Sub in an ordinary build.
	ordinaryVar = "FIXTURE_ORDINARY"
)

// The names of the fixture's module, files and build tag.
const (
	fixture          = "fixture"
	goModFile        = "go.mod"
	goSumFile        = "go.sum"
	arithFile        = "arith.go"
	arithTestFile    = "arith_test.go"
	genFile          = "gen.go"
	mutationTestFile = "mutation_test.go"
	checkDir         = "check"
	mutationTag      = "mutation"
)

// The modes of the fixture's files and directories.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// goMod is the go.mod of the fixture.
const goMod = "module fixture\n\ngo 1.21\n"

// linePrefix is what go test writes before the line of a mutant that Check
// reports in a top-level test: the indentation of the test's output.
const linePrefix = "    "

// moduleTimeout bounds the go test of the case that runs Check from a
// module that requires the engine.
const moduleTimeout = 5 * time.Minute

// goTestArgs are the arguments of that go test: the build tag of the test
// that calls Check, that test alone, no cached result and no deadline.
var goTestArgs = []string{"test", "-tags", mutationTag, "-run", "^TestMutation$", "-count=1", "-timeout", "0", "."}

// arith is a package with a function that its tests check, one that they
// call without checking, and one that they never call.
const arith = `package fixture

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

// arithTest checks Add, fails when failVar is set, sleeps in a run with a
// trace when slowVar is set, and checks its GOMAXPROCS against
// expectedProcsVar. It calls Sub, and checks it in an ordinary build when
// ordinaryVar is set.
var arithTest = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if os.Getenv(%[1]q) != "" {
		t.Fatal("the fixture fails")
	}
	if os.Getenv(%[2]q) != "" && os.Getenv(%[3]q) != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if want := os.Getenv(%[4]q); want != "" && os.Getenv(%[5]q) != want {
		t.Fatal("GOMAXPROCS is " + os.Getenv(%[5]q))
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	if os.Getenv(%[6]q) != "" && os.Getenv(%[7]q) == "" && Sub(5, 3) != 2 {
		t.Error("Sub(5, 3) != 2 in the ordinary build")
	}
	Sub(1, 1)
}
`, failVar, slowVar, render.TraceVar, expectedProcsVar, procsVar, ordinaryVar, spec.Load().Protocol.Instrumented)

// The lines of Check for arith under arithTest: the survivors of Sub, and
// the mutants of Unused, which no test covers.
var (
	survivors = []string{
		"arith.go:5:26: survived: return a - b became return 0 (sbr-zero)",
		"arith.go:5:33: survived: a - b became a + b (aor)",
	}
	uncovered = []string{
		"arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)",
		"arith.go:7:33: not covered: x * 2 became x / 2 (aor)",
	}
)

// summary is the log of Check for arith under arithTest.
const summary = "mutate: fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered"

// seat is a testing.TB that records what Check reports and reports nothing
// to the test that it embeds. Check writes a mutant's line to Output and
// marks the test failed with Fail, and states every other failure through
// Errorf or Fatalf. Fatalf returns, as Check's own return after it does.
// seat is not safe for concurrent use. Check calls it from one goroutine.
type seat struct {
	testing.TB
	errors   []string
	output   strings.Builder
	failed   bool
	fatal    string
	skipped  string
	logs     []string
	deadline time.Time
}

// Helper does nothing, because the seat states no source position.
func (*seat) Helper() {}

// Errorf records the message as a failure.
func (s *seat) Errorf(format string, a ...any) {
	s.errors = append(s.errors, fmt.Sprintf(format, a...))
}

// Fatalf records the message as the fatal failure, and returns.
func (s *seat) Fatalf(format string, a ...any) { s.fatal = fmt.Sprintf(format, a...) }

// Skip records the skip's message.
func (s *seat) Skip(args ...any) { s.skipped = fmt.Sprint(args...) }

// Log records the logged line.
func (s *seat) Log(args ...any) { s.logs = append(s.logs, fmt.Sprint(args...)) }

// Output returns the writer of the test's output, which the seat records.
func (s *seat) Output() io.Writer { return &s.output }

// Fail marks the test failed.
func (s *seat) Fail() { s.failed = true }

// Deadline returns the seat's deadline, and false for none.
func (s *seat) Deadline() (time.Time, bool) { return s.deadline, !s.deadline.IsZero() }

// failures returns the lines that fail the seat's test: the lines of its
// output, which fail it only together with Fail, and then its errors.
func failures(t *testing.T, s *seat) []string {
	t.Helper()
	var lines []string
	if s.output.Len() > 0 {
		assert.True(t, s.failed, "Check fails the test that it writes a line to")
		lines = strings.Split(strings.TrimSuffix(s.output.String(), "\n"), "\n")
	}
	return append(lines, s.errors...)
}

// write writes text into the file name of dir, a slash-separated path, and
// makes the file's directory.
func write(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	assert.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode), "the directory of "+name+" is made")
	assert.NoError(t, os.WriteFile(path, []byte(text), fileMode), name+" is written")
}

// enter writes the fixture's module into a new directory and makes it the
// working directory of the test process until t ends.
func enter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{goModFile: goMod, arithFile: arith, arithTestFile: arithTest} {
		write(t, dir, name, text)
	}
	t.Chdir(dir)
	return dir
}

func TestMutate(t *testing.T) {
	t.Parallel()

	t.Run("Check", func(t *testing.T) {
		t.Parallel()

		t.Run("fails the test that calls it in a module that requires the engine", func(t *testing.T) {
			t.Parallel()
			root, err := os.Getwd()
			assert.NoError(t, err, "the module's directory reads")
			// The fixture requires this module, so it needs this module's go
			// line and the checksums of this module's requirements.
			sums, err := os.ReadFile(filepath.Join(root, goSumFile))
			assert.NoError(t, err, "the module's checksums read")
			dir := t.TempDir()
			files := map[string]string{
				goModFile: "module fixture\n\ngo 1.27.0\n\nrequire go.dokimi.dev/mutate v0.0.0\n\n" +
					"replace go.dokimi.dev/mutate => " + root + "\n",
				goSumFile:     string(sums),
				arithFile:     arith,
				arithTestFile: arithTest,
				mutationTestFile: "//go:build " + mutationTag + "\n\npackage fixture\n\nimport (\n\t\"testing\"\n\n" +
					"\t\"go.dokimi.dev/mutate\"\n)\n\nfunc TestMutation(t *testing.T) {\n\tmutate.Check(t)\n}\n",
			}
			for name, text := range files {
				write(t, dir, name, text)
			}
			records := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), moduleTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", goTestArgs...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), recordDirVar+"="+records)
			out, err := cmd.CombinedOutput()
			exit := assert.ErrorAs[*exec.ExitError](t, err, "go test fails: "+string(out))
			assert.Equal(t, exit.ExitCode(), 1, "the test that calls Check fails")
			assert.ContainsInOrder(t, string(out), []string{
				"--- FAIL: TestMutation",
				"\n" + linePrefix + survivors[0] + "\n",
				"\n" + linePrefix + uncovered[1] + "\n",
				summary + "\n",
			}, "go test states the failed test, then each mutant's line and the summary in the test's output")
			_, err = os.Stat(filepath.Join(records, record.FileName(fixture)))
			assert.NoError(t, err, "Check writes the record into the directory of "+recordDirVar)
		})
	})

	t.Run("Workers", func(t *testing.T) {
		t.Parallel()

		t.Run("panics for fewer than one worker", func(t *testing.T) {
			t.Parallel()
			r := assert.Panics(t, func() { mutate.Workers(0) }, "Workers(0) panics")
			assert.Equal(t, r, any("mutate: Workers(0) states fewer than one worker"), "the panic states the count")
		})
	})
}

// TestMutateEnv changes the working directory and the environment of the
// test process, so neither it nor its subtests run in parallel.
func TestMutateEnv(t *testing.T) {
	t.Run("Check", func(t *testing.T) {
		t.Run("fails the test with one line for each undetected mutant at its position", func(t *testing.T) {
			enter(t)
			records := t.TempDir()
			t.Setenv(recordDirVar, records)
			s := &seat{TB: t}
			mutate.Check(s, mutate.Option{})
			assert.Equal(
				t,
				failures(t, s),
				slices.Concat(survivors, uncovered),
				"the lines follow the mutants' positions",
			)
			assert.Equal(t, s.logs, []string{summary}, "Check logs the summary")
			assert.Empty(t, s.fatal, "Check does not stop the test")
			assert.Empty(t, s.skipped, "and does not skip it")
			_, err := os.Stat(filepath.Join(records, record.FileName(fixture)))
			assert.NoError(t, err, "Check writes the record into the directory of "+recordDirVar)
		})

		t.Run("runs the mutants on the workers that Workers states", func(t *testing.T) {
			enter(t)
			t.Setenv(expectedProcsVar, strconv.Itoa(max(1, runtime.GOMAXPROCS(0)/2)))
			s := &seat{TB: t}
			mutate.Check(s, mutate.Workers(2))
			assert.Equal(t, failures(t, s), slices.Concat(survivors, uncovered),
				"each binary runs with GOMAXPROCS divided by the workers, and TestAdd passes")
		})

		t.Run("counts the tests of the packages that Suite names", func(t *testing.T) {
			dir := enter(t)
			write(t, dir, checkDir+"/check.go", "package check\n")
			write(t, dir, checkDir+"/check_test.go", "package check\n\nimport (\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n"+
				"func TestSub(t *testing.T) {\n\tif fixture.Sub(5, 3) != 2 {\n\t\tt.Error(\"Sub(5, 3) != 2\")\n\t}\n}\n")
			s := &seat{TB: t}
			mutate.Check(s, mutate.Suite("./"+checkDir))
			assert.Equal(t, failures(t, s), uncovered, "the suite's test kills Sub's mutants")
		})

		t.Run("confirms each survivor in its ordinary build under Confirm", func(t *testing.T) {
			// TestSub checks Sub only in an ordinary build, so the
			// confirmation kills both of Sub's survivors.
			enter(t)
			t.Setenv(ordinaryVar, "1")
			s := &seat{TB: t}
			mutate.Check(s, mutate.Confirm())
			assert.Equal(t, failures(t, s), uncovered, "the confirmations kill Sub's survivors")
		})

		t.Run("mutates the generated files under IncludeGenerated", func(t *testing.T) {
			dir := enter(t)
			write(
				t,
				dir,
				genFile,
				"// Code generated by hand. DO NOT EDIT.\n\npackage fixture\n\nfunc Double(x int) int { return x * 2 }\n",
			)
			s := &seat{TB: t}
			mutate.Check(s, mutate.IncludeGenerated())
			assert.Equal(t, failures(t, s), slices.Concat(survivors, uncovered, []string{
				"gen.go:5:26: not covered: return x * 2 became return 0 (sbr-zero)",
				"gen.go:5:33: not covered: x * 2 became x / 2 (aor)",
			}), "the generated file's mutants are the package's own")
			assert.Equal(t, s.logs, []string{"mutate: fixture: 2 of 8 mutants detected (25%): 2 killed, 2 survived, " +
				"4 not covered, 1 generated file with 2 mutants included"}, "the summary states the included file")
		})

		t.Run("restricts the run to the lines of the selection's variable", func(t *testing.T) {
			enter(t)
			t.Setenv(selection.Var, "arith.go:5-5,arith.go:6-6")
			s := &seat{TB: t}
			mutate.Check(s)
			assert.Equal(t, failures(t, s), survivors, "the run tests Sub alone")
		})

		t.Run("passes the test's deadline to the run", func(t *testing.T) {
			// The opening control run sleeps half a second, so a mutant's
			// deadline is above 7 seconds, and less than 10 are left.
			enter(t)
			t.Setenv(slowVar, "1")
			s := &seat{TB: t, deadline: time.Now().Add(10 * time.Second)}
			mutate.Check(s)
			lines := failures(t, s)
			assert.Length(
				t,
				lines,
				len(uncovered)+1,
				"Check states the uncovered mutants and the ones that did not run",
			)
			assert.Equal(t, lines[:len(uncovered)], uncovered, "the uncovered mutants have their verdicts")
			assert.HasPrefix(t, lines[len(uncovered)],
				"mutate: 4 mutants did not run: the caller's deadline leaves too little time",
				"and the deadline stops the four others")
		})

		t.Run("fails the test with each run error and the mutants that did not run", func(t *testing.T) {
			enter(t)
			t.Setenv(failVar, "1")
			s := &seat{TB: t}
			mutate.Check(s)
			lines := failures(t, s)
			assert.Length(t, lines, 2, "Check states the run error and the mutants that did not run")
			assert.HasPrefix(
				t,
				lines[0],
				"mutate: control-failed: the tests fail with no mutant active: TestAdd failed",
				"the first failure states the run error",
			)
			assert.Equal(t, lines[1], "mutate: 6 mutants did not run: the opening control run failed",
				"the second states the mutants that did not run")
		})

		t.Run("skips the test in a binary that Check started", func(t *testing.T) {
			t.Setenv(spec.Load().Protocol.Variable, "3")
			s := &seat{TB: t}
			mutate.Check(s)
			assert.Equal(t, s.skipped, "mutate: Check does not run in a test binary that Check started",
				"Check skips the test")
			assert.Empty(t, failures(t, s), "and fails nothing")
			assert.Empty(t, s.logs, "and logs nothing")
		})

		t.Run("stops the test for a selection that does not parse", func(t *testing.T) {
			t.Setenv(selection.Var, "arith.go:5-5,arith.go")
			s := &seat{TB: t}
			mutate.Check(s)
			assert.HasPrefix(
				t,
				s.fatal,
				"mutate: "+selection.Var+": selection: ",
				"the fatal failure names the variable",
			)
			assert.Empty(t, s.logs, "and Check logs nothing")
		})

		t.Run("stops the test when the run cannot make its work directory", func(t *testing.T) {
			enter(t)
			t.Setenv(tmpVar, filepath.Join(t.TempDir(), "missing"))
			s := &seat{TB: t}
			mutate.Check(s)
			assert.HasPrefix(t, s.fatal, "mutate: run: ", "the fatal failure states the run's error")
			assert.Empty(t, s.logs, "and Check logs nothing")
		})

		t.Run("fails the test when the record cannot be written", func(t *testing.T) {
			dir := enter(t)
			t.Setenv(recordDirVar, filepath.Join(dir, arithFile))
			s := &seat{TB: t}
			mutate.Check(s)
			lines := failures(t, s)
			assert.NotEmpty(t, lines, "Check fails the test")
			assert.HasPrefix(t, lines[len(lines)-1], "mutate: record: ", "the last failure states the record's error")
		})
	})
}
