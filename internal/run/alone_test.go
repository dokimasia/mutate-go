// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
)

// The variables of ordered's environment.
const (
	// runsVar names the file to which TestFirst appends the active mutant of
	// each mutant's run.
	runsVar = "FIXTURE_RUNS"
	// slowVar makes TestFirst sleep half a second in the opening control run.
	slowVar = "FIXTURE_SLOW"
	// dependsVar makes TestAdd fail when TestFirst did not run before it.
	dependsVar = "FIXTURE_DEPENDS"
)

// runsName is the name of the file of runsVar.
const runsName = "runs"

// runFlag starts the flag of a test binary that selects tests, which a run
// of one test alone passes and the control runs do not. It is the testing
// package's spelling, pinned.
const runFlag = "-test.run="

// aloneTrace returns a test main of the package fixture that runs stmt
// before the tests in a run that selects tests, as each test's run alone
// does, with trace the path of the run's trace file.
func aloneTrace(stmt string) string {
	return fmt.Sprintf(`package fixture

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	trace := os.Getenv(%q)
	for _, arg := range os.Args[1:] {
		if trace != "" && strings.HasPrefix(arg, %q) {
			%s
		}
	}
	os.Exit(m.Run())
}
`, render.TraceVar, runFlag, stmt)
}

// ordered is a test file of arith whose first test, TestFirst, checks
// nothing and appends the active mutant to the file of runsVar in each
// mutant's run. TestAdd checks Add, and fails without TestFirst before it
// when dependsVar is set. TestSub calls Sub without checking it.
var ordered = fmt.Sprintf(`package fixture

import (
	"os"
	"testing"
	"time"
)

var first bool

func TestFirst(t *testing.T) {
	first = true
	if os.Getenv(%[1]q) != "" && os.Getenv(%[2]q) != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if mutant := os.Getenv(%[3]q); mutant != "0" {
		f, err := os.OpenFile(os.Getenv(%[4]q), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString(mutant + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAdd(t *testing.T) {
	if os.Getenv(%[5]q) != "" && !first {
		t.Fatal("TestFirst did not run")
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	Sub(1, 1)
}
`, render.TraceVar, slowVar, protocol.Variable, runsVar, dependsVar)

func TestAlone(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("starts a mutant's run with the tests that executed its site", func(t *testing.T) {
			t.Parallel()
			// TestFirst runs before TestAdd and TestSub, and executes no site.
			// A mutant that TestAdd kills never runs it. A survivor runs it in
			// the whole suite after TestSub.
			rec, firsts := runOrdered(t, run.Config{}, nil)
			assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
			assert.Empty(t, rec.Errors, "the run states no run error")
			assert.Equal(t, firsts, 2, "TestFirst runs in the whole suites of Sub's survivors alone")
			assert.Equal(t, coveredBy(rec), [][]string{{"TestAdd"}, {"TestAdd"}, {"TestSub"}, {"TestSub"}, nil, nil},
				"each mutant names the test that executed its site alone")
		})

		tests := []struct {
			name string
			more map[string]string
			env  []string
		}{
			{name: "runs the whole suite for each mutant when a test fails alone", env: []string{dependsVar + "=1"}},
			{
				name: "runs the whole suite for each mutant when the trace of a test alone does not read",
				more: map[string]string{mainTestFile: aloneTrace("_ = os.Remove(trace)")},
			},
			{
				name: "runs the whole suite for each mutant when the trace of a test alone lacks the start mark",
				more: map[string]string{mainTestFile: aloneTrace("_ = os.Truncate(trace, 0)")},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec, firsts := runOrdered(t, run.Config{}, tt.more, tt.env...)
				assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
				assert.Empty(t, rec.Errors, "a run of a test alone is no control run")
				assert.Equal(t, firsts, 4, "TestFirst runs in the run of each mutant that runs")
				assert.Equal(t, coveredBy(rec), make([][]string, 6), "no mutant names a covering test")
			})
		}

		t.Run("runs no test alone for a test binary with fewer mutants to run than tests", func(t *testing.T) {
			t.Parallel()
			// The selection contains Add's two mutants, and the binary has
			// three tests.
			rec, firsts := runOrdered(
				t,
				run.Config{Lines: []selection.Lines{{Path: arithFile, First: 5, Last: 5}}},
				nil,
			)
			assert.Equal(t, verdicts(rec), addKilled+`Sub sbr-zero 0: not-selected
Sub aor 0: not-selected
Unused sbr-zero 0: not-selected
Unused aor 0: not-selected
`, "the mutants of Add alone run")
			assert.Equal(t, firsts, 2, "TestFirst runs in the run of each mutant that runs")
		})

		t.Run("runs no test alone when the caller's deadline leaves too little time", func(t *testing.T) {
			t.Parallel()
			// TestFirst sleeps half a second in the opening control run, so the
			// test binary's deadline is above 7 seconds. Less than 20 seconds
			// are left after the opening control run: twice the deadline, but
			// not three times.
			rec, firsts := runOrdered(t, run.Config{Deadline: time.Now().Add(20 * time.Second)}, nil, slowVar+"=1")
			assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
			assert.Equal(t, firsts, 4, "TestFirst runs in the run of each mutant that runs")
		})
	})
}

// runOrdered runs the engine on arith with the test file ordered, the files
// of more and cfg, with the variables env, and returns the record and the
// number of mutant runs in which TestFirst ran. The path of cfg's first
// range is relative to the module's directory.
func runOrdered(t *testing.T, cfg run.Config, more map[string]string, env ...string) (*record.Record, int) {
	t.Helper()
	dir := module(t, with(map[string]string{arithFile: arith, arithTestFile: ordered}, more))
	runs := filepath.Join(t.TempDir(), runsName)
	cfg.Env = append(os.Environ(), append(env, runsVar+"="+runs)...)
	if len(cfg.Lines) > 0 {
		cfg.Lines[0].Path = filepath.Join(dir, cfg.Lines[0].Path)
	}
	rec := runIn(t, dir, cfg)
	data, err := os.ReadFile(runs)
	if !errors.Is(err, fs.ErrNotExist) {
		assert.NoError(t, err, "the file of TestFirst's runs reads")
	}
	return rec, strings.Count(string(data), "\n")
}

// coveredBy returns the coveredBy of each of the record's mutants, in
// record order.
func coveredBy(rec *record.Record) [][]string {
	list := make([][]string, len(rec.Mutants))
	for i, m := range rec.Mutants {
		list[i] = m.CoveredBy
	}
	return list
}
