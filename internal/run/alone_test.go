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
			rec, firsts := runOrdered(t, run.Config{})
			assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
			assert.Empty(t, rec.Errors, "the run states no run error")
			assert.Equal(t, firsts, 2, "TestFirst runs in the whole suites of Sub's survivors alone")
			assert.Equal(t, coveredBy(rec), [][]string{{"TestAdd"}, {"TestAdd"}, {"TestSub"}, {"TestSub"}, nil, nil},
				"each mutant names the test that executed its site alone")
		})

		t.Run("runs the whole suite for each mutant when a test fails alone", func(t *testing.T) {
			t.Parallel()
			rec, firsts := runOrdered(t, run.Config{}, dependsVar+"=1")
			assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
			assert.Empty(t, rec.Errors, "a test that fails alone is no run error")
			assert.Equal(t, firsts, 4, "TestFirst runs in the run of each mutant that runs")
			assert.Equal(t, coveredBy(rec), make([][]string, 6), "no mutant names a covering test")
		})

		t.Run("runs no test alone for a test binary with fewer mutants to run than tests", func(t *testing.T) {
			t.Parallel()
			// The selection contains Add's two mutants, and the binary has
			// three tests.
			rec, firsts := runOrdered(t, run.Config{Lines: []selection.Lines{{Path: arithFile, First: 5, Last: 5}}})
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
			rec, firsts := runOrdered(t, run.Config{Deadline: time.Now().Add(20 * time.Second)}, slowVar+"=1")
			assert.Equal(t, verdicts(rec), arithVerdicts, "each mutant has the verdict of its run")
			assert.Equal(t, firsts, 4, "TestFirst runs in the run of each mutant that runs")
		})
	})
}

// runOrdered runs the engine on arith with the test file ordered and cfg,
// with the variables env, and returns the record and the number of mutant
// runs in which TestFirst ran. The path of cfg's first range is relative to
// the module's directory.
func runOrdered(t *testing.T, cfg run.Config, env ...string) (*record.Record, int) {
	t.Helper()
	dir := module(t, map[string]string{arithFile: arith, arithTestFile: ordered})
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
