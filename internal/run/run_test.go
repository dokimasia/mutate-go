// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

const arith = `package fixture

const Limit = 1 + 1

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

const arithTest = `package fixture

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	Sub(1, 1)
}
`

// subFirst is arith with Sub before Add. The keys of Add's mutants sort
// before the keys of Sub's, so the record order of the mutants that run is
// not the order of their keys.
const subFirst = `package fixture

func Sub(a, b int) int { return a - b }

func Add(a, b int) int { return a + b }
`

const arithVerdicts = `Add sbr-zero 0: killed [TestAdd]
Add aor 0: killed [TestAdd]
Sub sbr-zero 0: survived
Sub aor 0: survived
Unused sbr-zero 0: no-coverage
Unused aor 0: no-coverage
`

// count is a package whose loop's increment mutant never ends the loop.
const count = "package fixture\n\nfunc Count(n int) int {\n\tc := 0\n\tfor i := 0; i < n; i++ {\n\t\tc++\n\t}\n\treturn c\n}\n"

// countTest is a test file of count that checks Count.
const countTest = "package fixture\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n\tif Count(3) != 3 {\n\t\tt.Error(\"Count(3) != 3\")\n\t}\n}\n"

// slowCountTest is countTest with a second test, TestSlow, that sleeps a
// second in every run and calls nothing of count.
const slowCountTest = "package fixture\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\n" +
	"func TestCount(t *testing.T) {\n\tif Count(3) != 3 {\n\t\tt.Error(\"Count(3) != 3\")\n\t}\n}\n\n" +
	"func TestSlow(t *testing.T) {\n\ttime.Sleep(time.Second)\n}\n"

// countVerdicts are the verdicts of count's mutants under a test that
// checks Count.
const countVerdicts = `Count sbr-delete 0: killed [TestCount]
Count ror-boundary 0: killed [TestCount]
Count ror-false 0: killed [TestCount]
Count uoi-incdec 0: timed-out [TestCount]
Count uoi-incdec 1: killed [TestCount]
Count sbr-zero 0: killed [TestCount]
`

// slowOpening is a test of a fixture with Add whose opening control run
// sleeps half a second, which raises the deadline of a mutant's run above 7
// seconds. The mutant runs do not sleep.
const slowOpening = "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
	"\tif os.Getenv(\"DOKIMI_MUTATE_TRACE\") != \"\" {\n\t\ttime.Sleep(500 * time.Millisecond)\n\t}\n" +
	"\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n"

// notRun returns arithVerdicts with every mutant that runs not-run.
func notRun() string {
	return "Add sbr-zero 0: not-run\nAdd aor 0: not-run\nSub sbr-zero 0: not-run\nSub aor 0: not-run\n"
}

// scale is a package whose own test cannot tell a multiplication from a
// division, and whose package conformance tells them apart. The package
// other has tests that do not link scale.
var scale = map[string]string{
	"scale.go":                   "package fixture\n\nfunc Scale(x, factor int) int { return x * factor }\n",
	"scale_test.go":              "package fixture\n\nimport \"testing\"\n\nfunc TestScaleByOne(t *testing.T) {\n\tif Scale(2, 1) != 2 {\n\t\tt.Error(\"Scale(2, 1) != 2\")\n\t}\n}\n",
	"conformance/conformance.go": "package conformance\n",
	"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n" +
		"func TestScale(t *testing.T) {\n\tif fixture.Scale(6, 3) != 18 {\n\t\tt.Error(\"Scale(6, 3) != 18\")\n\t}\n}\n",
	"other/other.go":      "package other\n",
	"other/other_test.go": "package other\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n",
}

// confirmFiles is a package whose tests of double and triple run only in an
// ordinary build, as a test of a property of the build does, and whose
// other test calls double without checking the result. No test calls
// quarter.
var confirmFiles = map[string]string{
	"scale.go": "package fixture\n\nfunc double(n int) int { return n * 2 }\n\nfunc half(n int) int { return n / 2 }\n\n" +
		"func triple(n int) int { return n * 3 }\n\nfunc quarter(n int) int { return n / 4 }\n",
	"scale_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
		"func TestDouble(t *testing.T) {\n\tif os.Getenv(\"DOKIMI_MUTATE_INSTRUMENTED\") != \"\" {\n" +
		"\t\tt.Skip(\"a test of the ordinary build\")\n\t}\n\tif double(3) != 6 {\n\t\tt.Error(\"double(3) != 6\")\n\t}\n}\n\n" +
		"func TestCalls(t *testing.T) {\n\tdouble(3)\n\tif half(4) == 0 {\n\t\tt.Error(\"half(4) == 0\")\n\t}\n}\n\n" +
		"func TestTriple(t *testing.T) {\n\tif os.Getenv(\"DOKIMI_MUTATE_INSTRUMENTED\") != \"\" {\n" +
		"\t\tt.Skip(\"a test of the ordinary build\")\n\t}\n\tif triple(2) != 6 {\n\t\tt.Error(\"triple(2) != 6\")\n\t}\n}\n",
}

// ordered is a test file of arith whose first test, TestFirst, checks
// nothing and appends the active mutant to the file FIXTURE_MARKER in each
// mutant's run. TestAdd checks Add, and fails without TestFirst before it
// when FIXTURE_DEPENDS is set. TestSub calls Sub without checking it.
const ordered = `package fixture

import (
	"os"
	"testing"
	"time"
)

var first bool

func TestFirst(t *testing.T) {
	first = true
	if os.Getenv("DOKIMI_MUTATE_TRACE") != "" && os.Getenv("FIXTURE_SLOW") != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if mutant := os.Getenv("DOKIMI_MUTATE_MUTANT"); mutant != "0" {
		f, err := os.OpenFile(os.Getenv("FIXTURE_MARKER"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	if os.Getenv("FIXTURE_DEPENDS") != "" && !first {
		t.Fatal("TestFirst did not run")
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	Sub(1, 1)
}
`

// runOrdered runs the engine on arith with the test file ordered and cfg,
// with the variables env, and returns the record and the number of mutant
// runs in which TestFirst ran.
func runOrdered(t *testing.T, cfg run.Config, env ...string) (*record.Record, int) {
	t.Helper()
	dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": ordered})
	marker := filepath.Join(t.TempDir(), "marker")
	cfg.Env = append(os.Environ(), append(env, "FIXTURE_MARKER="+marker)...)
	if len(cfg.Lines) > 0 {
		cfg.Lines[0].Path = filepath.Join(dir, cfg.Lines[0].Path)
	}
	rec := runIn(t, dir, cfg)
	data, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return rec, strings.Count(string(data), "\n")
}

// broken is a file that does not compile in the package fixture.
const broken = "package fixture\n\nvar broken int = \"x\"\n"

// with returns a copy of files with the files of more added or replaced.
func with(files map[string]string, more map[string]string) map[string]string {
	out := map[string]string{}
	for name, text := range files {
		out[name] = text
	}
	for name, text := range more {
		out[name] = text
	}
	return out
}

func TestRun(t *testing.T) {
	t.Parallel()
	t.Run("Run", func(t *testing.T) {
		t.Parallel()
		t.Run("gives each mutant the verdict of its run and states the run", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			var mu sync.Mutex
			reported := map[string]string{}
			rec := runIn(t, dir, run.Config{Verdict: func(r *record.Record, m record.Mutant) {
				mu.Lock()
				defer mu.Unlock()
				reported[m.Key] = m.Verdict
				if r.Target.Name != "fixture" {
					t.Errorf("the callback receives the target %s, want fixture", r.Target.Name)
				}
			}})
			want(t, verdicts(rec), arithVerdicts)
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if rec.Record != "dokimi-mutate" || rec.Version != 1 || rec.Catalogue != spec.Load().Version ||
				rec.Engine.Name != "mutate-go" || rec.Toolchain != runtime.Version() ||
				rec.Target != (record.Target{Language: "go", Name: "fixture"}) || rec.Root != resolved {
				t.Errorf("record %+v", rec)
			}
			if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(rec.Inputs) || len(rec.Errors) != 0 ||
				rec.Selection != nil {
				t.Errorf("inputs %s, errors %v, selection %v", rec.Inputs, rec.Errors, rec.Selection)
			}
			wantSkip := record.Skip{
				File:   "arith.go",
				Start:  record.Position{Line: 3, Column: 15},
				End:    record.Position{Line: 3, Column: 20},
				Reason: "constant expression",
			}
			if len(rec.Skipped) != 1 || rec.Skipped[0] != wantSkip {
				t.Errorf("skipped %v, want %v", rec.Skipped, wantSkip)
			}
			for _, at := range []string{rec.StartedAt, rec.FinishedAt} {
				if _, err := time.Parse(time.RFC3339, at); err != nil {
					t.Error(err)
				}
			}
			if rec.Score == nil || *rec.Score != 2.0/6.0 {
				t.Errorf("score %v, want 2/6", rec.Score)
			}
			opening := rec.Control.Opening
			if opening.Sites != 6 || opening.SitesExecuted != 4 || opening.Seconds <= 0 || rec.Control.Closing == nil {
				t.Errorf("control %+v, closing %+v", opening, rec.Control.Closing)
			}
			if len(rec.Limits) != 1 || rec.Limits[0].Target != "fixture" {
				t.Fatalf("limits %+v, want the limits of the package's own test binary", rec.Limits)
			}
			limits := rec.Limits[0]
			if wantDeadline := 10*opening.Seconds + 2; limits.DeadlineSeconds < wantDeadline-0.001 ||
				limits.DeadlineSeconds > wantDeadline+0.001 {
				t.Errorf("deadline %v s, want %v s", limits.DeadlineSeconds, wantDeadline)
			}
			if opening.PeakBytes == nil || limits.MemoryCeilingBytes == nil ||
				*limits.MemoryCeilingBytes != 4**opening.PeakBytes+512<<20 {
				t.Errorf(
					"peak %v, ceiling %v, want the ceiling 4 times the peak plus 512 MiB",
					opening.PeakBytes,
					limits.MemoryCeilingBytes,
				)
			}
			for _, m := range rec.Mutants {
				ran := m.Verdict == record.Killed || m.Verdict == record.Survived
				if (m.Seconds != nil) != ran || reported[m.Key] != m.Verdict {
					t.Errorf("%s %s: seconds %v, reported %q", m.Scope, m.Kind, m.Seconds, reported[m.Key])
				}
			}
		})
		t.Run("starts the mutants' runs in the order of their keys", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": subFirst, "arith_test.go": arithTest})
			var ran []string
			rec := runIn(t, dir, run.Config{Verdict: func(_ *record.Record, m record.Mutant) {
				if m.Seconds != nil {
					ran = append(ran, m.Key)
				}
			}})
			var listed []string
			for _, m := range rec.Mutants {
				listed = append(listed, m.Key)
			}
			if len(ran) != 4 || !sort.StringsAreSorted(ran) || sort.StringsAreSorted(listed) {
				t.Errorf(
					"the runs ended in the order %v, and the record lists %v, want 4 runs in key order",
					ran,
					listed,
				)
			}
		})
		t.Run("confirms each survivor in its ordinary build", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(confirmFiles, nil))
			lines := []run.Lines{{Path: filepath.Join(dir, "scale.go"), First: 3, Last: 5}}
			rec := runIn(t, dir, run.Config{Confirm: true, Lines: lines})
			want(t, verdicts(rec)+codes(rec), `double sbr-zero 0: killed [TestDouble] confirmed
double aor 0: killed [TestDouble] confirmed
half sbr-zero 0: killed [TestCalls]
half aor 0: survived confirmed
triple sbr-zero 0: not-selected
triple aor 0: not-selected
quarter sbr-zero 0: not-selected
quarter aor 0: not-selected
`)
			if rec.Control.Ordinary == nil || rec.Control.Ordinary.Seconds <= 0 || rec.Control.Closing == nil {
				t.Errorf(
					"control %+v, ordinary %+v, closing %+v",
					rec.Control,
					rec.Control.Ordinary,
					rec.Control.Closing,
				)
			}
			for _, m := range rec.Mutants {
				if m.Verdict != record.NotSelected && m.Seconds == nil {
					t.Errorf("%s %s states no seconds", m.Scope, m.Kind)
				}
			}
		})
		t.Run("confirms each mutant without coverage in its ordinary build", func(t *testing.T) {
			t.Parallel()
			// Only TestTriple, which skips in the instrumented program, calls
			// triple. No test calls quarter.
			dir := module(t, with(confirmFiles, nil))
			lines := []run.Lines{{Path: filepath.Join(dir, "scale.go"), First: 7, Last: 9}}
			rec := runIn(t, dir, run.Config{Confirm: true, Lines: lines})
			want(t, verdicts(rec)+codes(rec), `double sbr-zero 0: not-selected
double aor 0: not-selected
half sbr-zero 0: not-selected
half aor 0: not-selected
triple sbr-zero 0: killed [TestTriple] confirmed
triple aor 0: killed [TestTriple] confirmed
quarter sbr-zero 0: no-coverage confirmed
quarter aor 0: no-coverage confirmed
`)
			for _, m := range rec.Mutants {
				if m.Verdict != record.NotSelected && m.Seconds == nil {
					t.Errorf("%s %s states no seconds", m.Scope, m.Kind)
				}
			}
		})
		t.Run("confirms no mutant without Confirm", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, with(confirmFiles, nil)), run.Config{})
			want(t, verdicts(rec)+codes(rec), `double sbr-zero 0: survived
double aor 0: survived
half sbr-zero 0: killed [TestCalls]
half aor 0: survived
triple sbr-zero 0: no-coverage
triple aor 0: no-coverage
quarter sbr-zero 0: no-coverage
quarter aor 0: no-coverage
`)
			if rec.Control.Ordinary != nil {
				t.Errorf("ordinary %+v, want no ordinary control run", rec.Control.Ordinary)
			}
		})
		t.Run("ends a mutant's run of a test binary at that binary's deadline", func(t *testing.T) {
			t.Parallel()
			// The opening control run of the package conformance sleeps a
			// second, so its deadline is above 12 seconds, and the package's
			// own is below 3. Count's increment mutant hangs in the package's
			// own test binary, and the conformance tests do not call Count.
			dir := module(t, map[string]string{
				"count.go":                   count,
				"count_test.go":              countTest,
				"conformance/conformance.go": "package conformance\n",
				"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n\n" +
					"\t\"fixture\"\n)\n\nvar _ = fixture.Count\n\nfunc TestSlow(t *testing.T) {\n" +
					"\tif os.Getenv(\"DOKIMI_MUTATE_TRACE\") != \"\" {\n\t\ttime.Sleep(time.Second)\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, verdicts(rec)+codes(rec), countVerdicts)
			if len(rec.Limits) != 2 || rec.Limits[0].Target != "fixture" ||
				rec.Limits[1].Target != "fixture/conformance" ||
				rec.Limits[1].DeadlineSeconds < rec.Limits[0].DeadlineSeconds+8 {
				t.Fatalf("limits %+v, want the package's own, and a longer deadline of fixture/conformance", rec.Limits)
			}
			for _, m := range rec.Mutants {
				if m.Verdict == record.TimedOut && *m.Seconds >= rec.Limits[0].DeadlineSeconds+5 {
					t.Errorf(
						"%s ran %v s, want the package's own deadline of %v s",
						m.Kind,
						*m.Seconds,
						rec.Limits[0].DeadlineSeconds,
					)
				}
			}
			var ceiling int64
			for _, l := range rec.Limits {
				if l.MemoryCeilingBytes == nil {
					t.Fatalf("limits %+v, want a memory ceiling of each test binary", rec.Limits)
				}
				ceiling = max(ceiling, *l.MemoryCeilingBytes)
			}
			if peak := rec.Control.Opening.PeakBytes; peak == nil || ceiling != 4**peak+512<<20 {
				t.Errorf("peak %v, largest ceiling %d, want the ceiling 4 times the peak plus 512 MiB", peak, ceiling)
			}
		})
		t.Run("starts a mutant's run with the tests that executed its site", func(t *testing.T) {
			t.Parallel()
			// TestFirst runs before TestAdd and TestSub, and executes no site.
			// A mutant that TestAdd kills never runs it. A survivor runs it in
			// the whole suite after TestSub.
			rec, firsts := runOrdered(t, run.Config{})
			want(t, verdicts(rec)+codes(rec), arithVerdicts)
			if firsts != 2 {
				t.Errorf("TestFirst ran in %d mutant runs, want 2: the whole suites of Sub's survivors", firsts)
			}
			want(
				t,
				coveredBy(rec),
				"Add sbr-zero: TestAdd\nAdd aor: TestAdd\nSub sbr-zero: TestSub\nSub aor: TestSub\n"+
					"Unused sbr-zero: \nUnused aor: \n",
			)
		})
		t.Run("runs the whole suite for each mutant when a test fails alone", func(t *testing.T) {
			t.Parallel()
			rec, firsts := runOrdered(t, run.Config{}, "FIXTURE_DEPENDS=1")
			want(t, verdicts(rec)+codes(rec), arithVerdicts)
			if firsts != 4 {
				t.Errorf("TestFirst ran in %d mutant runs, want 4, one for each mutant that runs", firsts)
			}
			want(
				t,
				coveredBy(rec),
				"Add sbr-zero: \nAdd aor: \nSub sbr-zero: \nSub aor: \nUnused sbr-zero: \nUnused aor: \n",
			)
		})
		t.Run("runs no test alone for a test binary with fewer mutants to run than tests", func(t *testing.T) {
			t.Parallel()
			// The selection contains Add's two mutants, and the binary has
			// three tests.
			rec, firsts := runOrdered(t, run.Config{Lines: []run.Lines{{Path: "arith.go", First: 5, Last: 5}}})
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n"+
				"Sub sbr-zero 0: not-selected\nSub aor 0: not-selected\nUnused sbr-zero 0: not-selected\nUnused aor 0: not-selected\n")
			if firsts != 2 {
				t.Errorf("TestFirst ran in %d mutant runs, want 2, one for each mutant that runs", firsts)
			}
		})
		t.Run("runs no test alone when the caller's deadline leaves too little time", func(t *testing.T) {
			t.Parallel()
			// TestFirst sleeps half a second in the opening control run, so the
			// test binary's deadline is above 7 seconds. Less than 20 seconds
			// are left after the opening control run: twice the deadline, but
			// not three times.
			rec, firsts := runOrdered(t, run.Config{Deadline: time.Now().Add(20 * time.Second)}, "FIXTURE_SLOW=1")
			want(t, verdicts(rec)+codes(rec), arithVerdicts)
			if firsts != 4 {
				t.Errorf("TestFirst ran in %d mutant runs, want 4, one for each mutant that runs", firsts)
			}
		})
		t.Run("gives timed-out to a mutant that hangs past the deadline", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"count.go": count, "count_test.go": countTest})
			rec := runIn(t, dir, run.Config{Workers: 2})
			want(t, verdicts(rec), countVerdicts)
		})
		t.Run("ends the run of a mutant's covering tests at a deadline from their own times", func(t *testing.T) {
			t.Parallel()
			// TestSlow sleeps a second in every run, which raises the test
			// binary's deadline above 12 seconds. The mutant that hangs in
			// TestCount, whose run alone takes milliseconds, ends about 2 seconds
			// after its start.
			dir := module(t, map[string]string{"count.go": count, "count_test.go": slowCountTest})
			rec := runIn(t, dir, run.Config{})
			want(t, verdicts(rec), countVerdicts)
			for _, m := range rec.Mutants {
				if m.Verdict == record.TimedOut && (m.Seconds == nil || *m.Seconds > rec.Limits[0].DeadlineSeconds/2) {
					t.Errorf(
						"the hang ran %v s, against the binary's deadline of %v s",
						m.Seconds,
						rec.Limits[0].DeadlineSeconds,
					)
				}
			}
		})
		t.Run("names only the running test of a mutant that hangs while parallel subtests pause", func(t *testing.T) {
			t.Parallel()
			// The parallel subtests pause until TestCount's function returns,
			// so they never continue while Count hangs.
			dir := module(t, map[string]string{
				"count.go": count,
				"count_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n" +
					"\tt.Run(\"a\", func(t *testing.T) { t.Parallel() })\n\tt.Run(\"b\", func(t *testing.T) { t.Parallel() })\n" +
					"\tif Count(3) != 3 {\n\t\tt.Error(\"Count(3) != 3\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, verdicts(rec), countVerdicts)
		})
		t.Run("ends a mutant's run at the first failed test while another test runs", func(t *testing.T) {
			t.Parallel()
			// Under a mutant, TestSlow sleeps for a minute in parallel with
			// TestAdd, which fails at once.
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
					"func TestAdd(t *testing.T) {\n\tt.Parallel()\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n\n" +
					"func TestSlow(t *testing.T) {\n\tt.Parallel()\n\tif os.Getenv(\"DOKIMI_MUTATE_MUTANT\") != \"0\" {\n" +
					"\t\ttime.Sleep(time.Minute)\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{Procs: 2})
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
			for _, m := range rec.Mutants {
				if m.Seconds == nil || *m.Seconds >= rec.Limits[0].DeadlineSeconds {
					t.Errorf(
						"%s took %v s, want less than the deadline of %v s",
						m.Kind,
						m.Seconds,
						rec.Limits[0].DeadlineSeconds,
					)
				}
			}
		})
		t.Run("gives timed-out to a mutant that hangs while the package initializes", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"steps.go":      "package fixture\n\nvar steps = count(3)\n\nfunc count(n int) int {\n\tc := 0\n\tfor c != n {\n\t\tc++\n\t}\n\treturn c\n}\n\nfunc Steps() int { return steps }\n",
				"steps_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestSteps(t *testing.T) {\n\tif Steps() != 3 {\n\t\tt.Error(\"Steps() != 3\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{Workers: 2})
			want(t, verdicts(rec), `count sbr-delete 0: killed [TestSteps]
count ror-true 0: timed-out
count ror-false 0: killed [TestSteps]
count uoi-incdec 0: timed-out
count sbr-zero 0: killed [TestSteps]
Steps sbr-zero 0: killed [TestSteps]
`)
		})
		t.Run("kills a mutant whose test ends the binary with status 0", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tos.Exit(0)\n\t}\n}\n",
			})
			want(
				t,
				verdicts(runIn(t, dir, run.Config{})),
				"Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n",
			)
		})
		t.Run("states in the inputs digest the files that the runs start from", func(t *testing.T) {
			t.Parallel()
			// The test writes testdata/written.txt into the package directory
			// in every run.
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
					"\tif err := os.MkdirAll(\"testdata\", 0o755); err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
					"\tif err := os.WriteFile(\"testdata/written.txt\", []byte(\"x\"), 0o644); err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
					"\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
			})
			first := runIn(t, dir, run.Config{})
			second := runIn(t, dir, run.Config{})
			third := runIn(t, dir, run.Config{})
			if first.Inputs == second.Inputs || second.Inputs != third.Inputs {
				t.Errorf(
					"digests %s, %s and %s, want the first to differ and the two that start with the written file to be equal",
					first.Inputs,
					second.Inputs,
					third.Inputs,
				)
			}
		})
		t.Run("names the running test of a mutant whose test exits with status 1", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tos.Exit(1)\n\t}\n}\n",
			})
			want(
				t,
				verdicts(runIn(t, dir, run.Config{})),
				"Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n",
			)
		})
		t.Run("divides GOMAXPROCS among the workers", func(t *testing.T) {
			t.Parallel()
			procs := strconv.Itoa(max(1, runtime.GOMAXPROCS(0)/2))
			dir := module(t, map[string]string{
				"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": procsTest,
			})
			env := append(withoutKey(os.Environ(), "GOMAXPROCS"), "FIXTURE_PROCS="+procs)
			rec := runIn(t, dir, run.Config{Env: env, Workers: 2})
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
		})
		t.Run("runs each test binary with GOMAXPROCS set to Procs, and once more divided among the workers",
			func(t *testing.T) {
				t.Parallel()
				dir := module(t, map[string]string{
					"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
					"add_test.go": procsTest,
				})
				for _, tt := range []struct{ procs, workers, want int }{{3, 1, 3}, {3, 2, 1}, {1, 2, 1}} {
					env := append(os.Environ(), "FIXTURE_PROCS="+strconv.Itoa(tt.want))
					rec := runIn(t, dir, run.Config{Env: env, Procs: tt.procs, Workers: tt.workers})
					want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
				}
			},
		)
		t.Run("lists every mutant in the record before the first verdict", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go": strings.Replace(
					arith,
					"func Add",
					"//dokimi:mutate-skip aor: Sub checks it\nfunc Add",
					1,
				),
				"arith_test.go": arithTest,
			})
			first := -1
			rec := runIn(t, dir, run.Config{Verdict: func(r *record.Record, m record.Mutant) {
				if first < 0 {
					first = len(r.Mutants)
					if m.Verdict != record.Suppressed {
						t.Errorf("the first verdict is %s, want the suppression of Add's aor", m.Verdict)
					}
				}
			}})
			if first != 6 || len(rec.Mutants) != 6 {
				t.Errorf(
					"the record lists %d mutants at the first verdict and %d at the end, want 6",
					first,
					len(rec.Mutants),
				)
			}
		})
		t.Run("marks the mutants outside the selection", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			reported := 0
			rec := runIn(
				t,
				dir,
				run.Config{
					Lines: []run.Lines{{Path: filepath.Join(dir, "arith.go"), First: 7, Last: 7}},
					Verdict: func(*record.Record, record.Mutant) {
						reported++
					},
				},
			)
			if reported != len(rec.Mutants) {
				t.Errorf("reported %d verdicts of %d mutants", reported, len(rec.Mutants))
			}
			want(t, verdicts(rec), `Add sbr-zero 0: not-selected
Add aor 0: not-selected
Sub sbr-zero 0: survived
Sub aor 0: survived
Unused sbr-zero 0: not-selected
Unused aor 0: not-selected
`)
		})
		t.Run("states the ranges of the selection in the package's files", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runIn(t, dir, run.Config{Lines: []run.Lines{
				{Path: filepath.Join(dir, "arith.go"), First: 7, Last: 7},
				{Path: filepath.Join(dir, "other", "other.go"), First: 1, Last: 9},
			}})
			if len(rec.Selection) != 1 || rec.Selection[0] != (record.Range{File: "arith.go", First: 7, Last: 7}) {
				t.Errorf("selection %v, want the range of arith.go alone", rec.Selection)
			}
		})
		t.Run("starts only the first mutants in key order under Sample", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runIn(t, dir, run.Config{Sample: 2})
			var ran, stopped []string
			for _, m := range rec.Mutants {
				switch m.Verdict {
				case record.NotRun:
					stopped = append(stopped, m.Key)
					if m.Reason != "the caller limits the run to 2 mutants" {
						t.Errorf("%s is not-run for %q", m.Key, m.Reason)
					}
				case record.Killed, record.Survived:
					ran = append(ran, m.Key)
				}
			}
			sort.Strings(ran)
			sort.Strings(stopped)
			if len(ran) != 2 || len(stopped) != 2 || ran[1] > stopped[0] || rec.Sample == nil ||
				rec.Sample.Before != stopped[0] {
				t.Errorf("ran %v, stopped %v and sample %+v, want the two least keys run", ran, stopped, rec.Sample)
			}
		})
		t.Run("asks Admit for the workers times the largest memory ceiling before the runs", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			var asked int64
			released := false
			admit := func(_ context.Context, bytes int64) (func(), bool) {
				asked = bytes
				return func() { released = true }, true
			}
			rec := runIn(t, dir, run.Config{Workers: 2, Admit: admit})
			want(t, verdicts(rec), arithVerdicts)
			var ceiling int64
			if c := rec.Limits[0].MemoryCeilingBytes; c != nil {
				ceiling = *c
			}
			if asked != 2*ceiling || !released {
				t.Errorf("Admit got %d bytes and released %v, want %d and released", asked, released, 2*ceiling)
			}
		})
		t.Run("lists the generated files that the run leaves out", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"gen.go": "// Code generated by hand. DO NOT EDIT.\n\npackage fixture\n\nfunc Double(x int) int { return x * 2 }\n",
				"gen_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n" +
					"\tif Double(2) != 4 {\n\t\tt.Error(\"Double(2) != 4\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			if len(rec.Generated) != 1 || rec.Generated[0] != (record.Generated{File: "gen.go", Mutants: 2}) ||
				len(rec.Mutants) != 0 {
				t.Errorf(
					"generated %v and %d mutants, want gen.go with 2 mutants and none to run",
					rec.Generated,
					len(rec.Mutants),
				)
			}
		})
		t.Run("selects no mutant under a selection without a range", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runIn(t, dir, run.Config{Lines: []run.Lines{}})
			want(t, verdicts(rec), `Add sbr-zero 0: not-selected
Add aor 0: not-selected
Sub sbr-zero 0: not-selected
Sub aor 0: not-selected
Unused sbr-zero 0: not-selected
Unused aor 0: not-selected
`)
			if rec.Selection == nil || len(rec.Selection) != 0 || rec.Control != nil {
				t.Errorf(
					"selection %v and control %+v, want an empty selection and no control run",
					rec.Selection,
					rec.Control,
				)
			}
		})
		t.Run("counts the tests of the other packages of the suite that link the package", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, nil))
			own := runIn(t, dir, run.Config{})
			want(t, verdicts(own), "Scale sbr-zero 0: killed [TestScaleByOne]\nScale aor 0: survived\n")
			rec := runIn(t, dir, run.Config{Suite: []string{"./..."}})
			want(
				t,
				verdicts(rec)+codes(rec),
				"Scale sbr-zero 0: killed [TestScaleByOne]\nScale aor 0: killed [fixture/conformance: TestScale]\n",
			)
			if len(rec.Suite) != 1 || rec.Suite[0] != "fixture/conformance" || own.Suite != nil {
				t.Errorf("suite %q and, without one, %q, want [fixture/conformance] and nil", rec.Suite, own.Suite)
			}
			if rec.Inputs == own.Inputs || rec.Control.Closing == nil || rec.Control.Opening.SitesExecuted != 2 {
				t.Errorf("inputs %s and %s, control %+v", rec.Inputs, own.Inputs, rec.Control)
			}
		})
		t.Run("runs for a mutant only the test binaries that executed its site", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"triple.go": "package fixture\n\nfunc Triple(x int) int { return x * 3 }\n",
				"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n" +
					"func TestTriple(t *testing.T) {\n\tif fixture.Triple(2) != 6 {\n\t\tt.Error(\"Triple(2) != 6\")\n\t}\n}\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, verdicts(rec)+codes(rec), "Scale sbr-zero 0: killed [TestScaleByOne]\nScale aor 0: survived\n"+
				"Triple sbr-zero 0: killed [fixture/conformance: TestTriple]\nTriple aor 0: killed [fixture/conformance: TestTriple]\n")
		})
		t.Run("runs the suite's other test binaries when the package has no test file", func(t *testing.T) {
			t.Parallel()
			files := with(scale, nil)
			delete(files, "scale_test.go")
			rec := runIn(t, module(t, files), run.Config{Suite: []string{"./conformance"}})
			want(
				t,
				verdicts(rec)+codes(rec),
				"Scale sbr-zero 0: killed [fixture/conformance: TestScale]\nScale aor 0: killed [fixture/conformance: TestScale]\n",
			)
		})
		t.Run("gives no-coverage to every mutant of a package without a test file", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{"arith.go": arith}), run.Config{})
			want(
				t,
				verdicts(rec),
				"Add sbr-zero 0: no-coverage\nAdd aor 0: no-coverage\nSub sbr-zero 0: no-coverage\nSub aor 0: no-coverage\n"+
					"Unused sbr-zero 0: no-coverage\nUnused aor 0: no-coverage\n",
			)
			if rec.Control != nil || rec.Score == nil || *rec.Score != 0 {
				t.Errorf("control %v, score %v, want no control run and the score 0", rec.Control, rec.Score)
			}
		})
	})
	t.Run("Run errors", func(t *testing.T) {
		t.Parallel()
		t.Run("states the load error of a package that does not type-check", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"bad.go": "package fixture\n\nvar V int = \"x\"\n"})
			rec := runIn(t, dir, run.Config{})
			if codes(rec) != "load" || len(rec.Mutants) != 0 || rec.Score != nil {
				t.Errorf("errors %v, %d mutants, score %v", rec.Errors, len(rec.Mutants), rec.Score)
			}
		})
		t.Run("names a package that does not load by its import path", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"bad.go": "package fixture\n\nimport _ \"fixture/missing\"\n"})
			rec := runIn(t, dir, run.Config{})
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if codes(rec) != "load" || rec.Target.Name != "fixture" || rec.Root != resolved {
				t.Errorf(
					"errors %v, target %s, root %s, want load, fixture and %s",
					rec.Errors,
					rec.Target.Name,
					rec.Root,
					resolved,
				)
			}
		})
		t.Run("states an annotation error and runs no mutant", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go": strings.Replace(
					arith,
					"func Sub",
					"//dokimi:mutate-skip ror: nothing to skip\nfunc Sub",
					1,
				),
				"arith_test.go": arithTest,
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "stale-annotation")
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the run stopped at an annotation error" {
				t.Errorf("reason %q", rec.Mutants[0].Reason)
			}
		})
		t.Run("states the build error of a test file that does not compile", func(t *testing.T) {
			t.Parallel()
			dir := module(
				t,
				map[string]string{"arith.go": arith, "arith_test.go": arithTest + "\nvar broken int = \"x\"\n"},
			)
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "build")
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			msg := rec.Errors[0].Message
			if !strings.HasPrefix(msg, "the tests do not build from the unchanged source: ") ||
				!strings.Contains(msg, "arith_test.go") || rec.Mutants[0].Reason != "the tests do not build" {
				t.Errorf("message %q, reason %q", msg, rec.Mutants[0].Reason)
			}
		})
		t.Run("states the build error of an instrumented build that fails while the unchanged source builds",
			func(t *testing.T) {
				t.Parallel()
				// The test file declares a name of the instrumentation's helper
				// file, which only the instrumented build compiles.
				dir := module(t, map[string]string{
					"arith.go":       arith,
					"arith_test.go":  arithTest,
					"helper_test.go": "package fixture\n\nvar _mutateActive = 0\n",
				})
				rec := runIn(t, dir, run.Config{})
				want(t, codes(rec), "build")
				msg, reason := rec.Errors[0].Message, rec.Mutants[0].Reason
				if !strings.HasPrefix(msg, "the instrumented build fails, and the unchanged source builds: ") ||
					!strings.Contains(msg, "_mutateActive") || reason != "the instrumented build failed" {
					t.Errorf("message %q, reason %q", msg, reason)
				}
			},
		)
		t.Run("states the build error of a package that declares a name of the instrumentation", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go":      arith,
				"arith_test.go": arithTest,
				"names.go":      "package fixture\n\nvar _mutateActive = 0\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "build")
			msg, reason := rec.Errors[0].Message, rec.Mutants[0].Reason
			if !strings.HasPrefix(msg, "render: the type checker rejects the instrumented package: ") ||
				!strings.Contains(msg, "_mutateActive") || reason != "the instrumented build failed" {
				t.Errorf("message %q, reason %q", msg, reason)
			}
		})
		t.Run("runs no test when no mutant is left to run", func(t *testing.T) {
			t.Parallel()
			// TestBroken fails with no mutant active, and the selection
			// contains no mutant.
			dir := module(t, map[string]string{
				"arith.go":      arith,
				"arith_test.go": arithTest + "\nfunc TestBroken(t *testing.T) {\n\tt.Fatal(\"broken\")\n}\n",
			})
			lines := []run.Lines{{Path: filepath.Join(dir, "arith.go"), First: 1, Last: 1}}
			rec := runIn(t, dir, run.Config{Lines: lines})
			want(t, verdicts(rec)+codes(rec), `Add sbr-zero 0: not-selected
Add aor 0: not-selected
Sub sbr-zero 0: not-selected
Sub aor 0: not-selected
Unused sbr-zero 0: not-selected
Unused aor 0: not-selected
`)
			if rec.Control != nil || rec.Failed() {
				t.Errorf("control %+v, failed %v, want no control run and a run that passes", rec.Control, rec.Failed())
			}
		})
		t.Run("states control-failed when a test fails with no mutant active", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go": arith,
				"arith_test.go": arithTest + "\nfunc TestBroken(t *testing.T) {\n\tfor i := 0; i < 10; i++ {\n" +
					"\t\tt.Log(\"line\", i, \"" + strings.Repeat("x", 1000) + "\")\n\t}\n\tt.Fatal(\"broken\")\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "control-failed")
			prefix := "the tests fail with no mutant active: TestBroken failed\n"
			if msg := rec.Errors[0].Message; !strings.HasPrefix(msg, prefix) || !strings.Contains(msg, "broken") ||
				len(msg) != len(prefix)+8<<10 {
				t.Errorf("message of %d bytes %q", len(msg), msg)
			}
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			if rec.Control != nil || rec.Score != nil {
				t.Errorf("control %v, score %v", rec.Control, rec.Score)
			}
		})
		t.Run("states control-failed without a test when the test binary fails before its tests", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go": arith,
				"main_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
					"func TestMain(m *testing.M) {\n\tos.Exit(1)\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "control-failed")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail with no mutant active: the test binary failed\n",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("states not-instrumented when the trace lacks the start mark", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"arith.go": arith,
				"main_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
					"func TestMain(m *testing.M) {\n\tif trace := os.Getenv(\"DOKIMI_MUTATE_TRACE\"); trace != \"\" {\n\t\t_ = os.Remove(trace)\n\t}\n\tos.Exit(m.Run())\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "not-instrumented")
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
		})
		t.Run("states closing-control-failed when a mutant run changes what the tests depend on", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\n" +
					"func TestMarker(t *testing.T) {\n\tmarker := filepath.Join(os.Getenv(\"FIXTURE_MARKER_DIR\"), \"marker\")\n" +
					"\tif _, err := os.Stat(marker); err == nil {\n\t\tt.Fatal(\"the marker exists\")\n\t}\n" +
					"\tif err := os.WriteFile(marker, nil, 0o644); err != nil {\n\t\tt.Fatal(err)\n\t}\n\tAdd(2, 3)\n}\n",
			})
			rec := runIn(t, dir, run.Config{Env: append(os.Environ(), "FIXTURE_MARKER_DIR="+t.TempDir())})
			want(t, codes(rec), "closing-control-failed")
			want(t, verdicts(rec), "Add sbr-zero 0: killed [TestMarker]\nAdd aor 0: killed [TestMarker]\n")
			if rec.Control.Closing != nil || rec.Score != nil {
				t.Errorf("closing %v, score %v", rec.Control.Closing, rec.Score)
			}
		})
		t.Run("states changed-files with each data file that the runs added, changed or removed", func(t *testing.T) {
			t.Parallel()
			// Every run of the package's own test writes out.txt and data.txt
			// and removes removed.txt. Every run of the other test writes
			// testdata/c.txt into the package conformance.
			dir := module(t, with(scale, map[string]string{
				"data.txt":    "before",
				"removed.txt": "x",
				"scale_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestScaleByOne(t *testing.T) {\n" +
					"\t_ = os.WriteFile(\"out.txt\", []byte(\"x\"), 0o644)\n\t_ = os.WriteFile(\"data.txt\", []byte(\"after\"), 0o644)\n" +
					"\t_ = os.Remove(\"removed.txt\")\n\tif Scale(2, 1) != 2 {\n\t\tt.Error(\"Scale(2, 1) != 2\")\n\t}\n}\n",
				"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n" +
					"func TestScale(t *testing.T) {\n\t_ = os.MkdirAll(\"testdata\", 0o755)\n\t_ = os.WriteFile(\"testdata/c.txt\", nil, 0o644)\n" +
					"\tif fixture.Scale(6, 3) != 18 {\n\t\tt.Error(\"Scale(6, 3) != 18\")\n\t}\n}\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, codes(rec), "changed-files")
			want(
				t,
				rec.Errors[0].Message,
				"the runs added, changed or removed these files: data.txt, out.txt, removed.txt, conformance/testdata/c.txt",
			)
			if rec.Score != nil {
				t.Errorf("score %v, want none for a run that changed files", *rec.Score)
			}
		})
		t.Run("states changed-files when a data file does not read after the runs", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
					"\t_ = os.Symlink(\"missing\", \"dangling\")\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{})
			want(t, codes(rec), "changed-files")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(msg, "the files do not read after the runs: record: ") {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("states the load error of a package of the suite that does not list", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{"broken/broken.go": "pkg broken\n"}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./broken"}})
			want(t, codes(rec)+"\n"+verdicts(rec), "load\nScale sbr-zero 0: not-run\nScale aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the packages of the suite do not load" {
				t.Errorf("reason %q", rec.Mutants[0].Reason)
			}
		})
		t.Run("states the load error when a data file of a package of the suite does not read", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, nil))
			if err := os.Symlink("missing", filepath.Join(dir, "conformance", "dangling")); err != nil {
				t.Fatal(err)
			}
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, codes(rec), "load")
		})
		t.Run("states the build error of a package of the suite whose tests do not compile", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"conformance/conformance_test.go": scale["conformance/conformance_test.go"] + "\nvar broken int = \"x\"\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, codes(rec)+"\n"+verdicts(rec), "build\nScale sbr-zero 0: not-run\nScale aor 0: not-run\n")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests of fixture/conformance do not build from the unchanged source: ",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("names the package of the suite whose test fails with no mutant active", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"testing\"\n\n\t_ \"fixture\"\n)\n\n" +
					"func TestBroken(t *testing.T) {\n\tt.Error(\"broken\")\n}\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, codes(rec), "control-failed")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail with no mutant active: fixture/conformance: TestBroken failed\n",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("names the package of the suite whose test binary does not trace", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"conformance/main_test.go": "package conformance\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
					"func TestMain(m *testing.M) {\n\tif trace := os.Getenv(\"DOKIMI_MUTATE_TRACE\"); trace != \"\" {\n" +
					"\t\t_ = os.Remove(trace)\n\t}\n\tos.Exit(m.Run())\n}\n",
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{"./conformance"}})
			want(t, codes(rec), "not-instrumented")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the opening control run's trace of fixture/conformance has no start mark",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("names the package of the suite whose test fails after the mutant runs", func(t *testing.T) {
			t.Parallel()
			dir := module(t, with(scale, map[string]string{
				"conformance/conformance_test.go": "package conformance\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n\n" +
					"\t\"fixture\"\n)\n\nfunc TestMarker(t *testing.T) {\n" +
					"\tmarker := filepath.Join(os.Getenv(\"FIXTURE_MARKER_DIR\"), \"marker\")\n" +
					"\tif _, err := os.Stat(marker); err == nil {\n\t\tt.Fatal(\"the marker exists\")\n\t}\n" +
					"\tif err := os.WriteFile(marker, nil, 0o644); err != nil {\n\t\tt.Fatal(err)\n\t}\n\tfixture.Scale(6, 3)\n}\n",
			}))
			env := append(os.Environ(), "FIXTURE_MARKER_DIR="+t.TempDir())
			rec := runIn(t, dir, run.Config{Env: env, Suite: []string{"./conformance"}})
			want(t, codes(rec), "closing-control-failed")
			want(
				t,
				verdicts(rec),
				"Scale sbr-zero 0: killed [TestScaleByOne]\nScale aor 0: killed [fixture/conformance: TestMarker]\n",
			)
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail with no mutant active after the mutant runs: fixture/conformance: TestMarker failed\n",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run(
			"starts no mutant when the time left is shorter than its deadline and the closing run's",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and less than 10 are
				// left after the opening control run.
				dir := module(t, map[string]string{
					"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
					"add_test.go": slowOpening,
				})
				rec := runIn(t, dir, run.Config{Deadline: time.Now().Add(10 * time.Second)})
				want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
				if rec.Mutants[0].Reason != "the caller's deadline leaves too little time for the mutant and the closing control run" ||
					rec.Control == nil ||
					rec.Control.Closing == nil ||
					len(rec.Errors) != 0 {
					t.Errorf("reason %q, control %+v, errors %v", rec.Mutants[0].Reason, rec.Control, rec.Errors)
				}
				if rec.Sample == nil || rec.Sample.Score != nil {
					t.Errorf("sample %+v, want a sample without a score", rec.Sample)
				}
			},
		)
		t.Run("starts a mutant while the time left covers its deadline and the closing run's", func(t *testing.T) {
			t.Parallel()
			// A mutant's deadline is above 7 seconds, and less than 24 are
			// left after the opening control run: twice the deadline, but
			// not twice the deadline and the backup delay.
			dir := module(t, map[string]string{
				"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": slowOpening,
			})
			rec := runIn(t, dir, run.Config{Deadline: time.Now().Add(24 * time.Second)})
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
		})
		t.Run("stops starting mutants when the time left no longer covers a mutant", func(t *testing.T) {
			t.Parallel()
			// TestAdd sleeps half a second in the opening control run, so the
			// test binary's deadline is above 7 seconds, and a minute under a
			// mutant, so the first mutant runs until that deadline. Less than
			// 20 seconds are left after the opening control run: twice the
			// deadline, but not three times, so the time left falls below twice
			// the deadline while the first mutant runs.
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": strings.Replace(
					slowOpening,
					"\tif Add(2, 3)",
					"\tif os.Getenv(\"DOKIMI_MUTATE_MUTANT\") != \"0\" {\n\t\ttime.Sleep(time.Minute)\n\t}\n\tif Add(2, 3)",
					1,
				),
			})
			var mu sync.Mutex
			var order []string
			rec := runIn(t, dir, run.Config{
				Deadline: time.Now().Add(20 * time.Second),
				Verdict: func(_ *record.Record, m record.Mutant) {
					mu.Lock()
					defer mu.Unlock()
					order = append(order, m.Verdict)
				},
			})
			// The mutant that did not start gets its verdict while the first
			// one still runs.
			want(t, strings.Join(order, " "), "not-run timed-out")
			first, second := rec.Mutants[0], rec.Mutants[1]
			if second.Key < first.Key {
				first, second = second, first
			}
			if first.Verdict != record.TimedOut || second.Verdict != record.NotRun || second.Reason !=
				"the caller's deadline leaves too little time for the mutant and the closing control run" {
				t.Errorf("first %+v, second %+v, want the first timed out and the second not run", first, second)
			}
		})
		t.Run("runs no control run when the caller's deadline has passed", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runIn(t, dir, run.Config{Deadline: time.Now().Add(-time.Second)})
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller's deadline leaves too little time for the opening control run" ||
				rec.Control != nil || len(rec.Errors) != 0 {
				t.Errorf("reason %q, control %v, errors %v", rec.Mutants[0].Reason, rec.Control, rec.Errors)
			}
		})
		t.Run("states no run error when the caller's deadline ends the opening control run", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\n" +
					"func TestAdd(t *testing.T) {\n\ttime.Sleep(time.Minute)\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{Deadline: time.Now().Add(4 * time.Second)})
			want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller's deadline leaves too little time for the opening control run" ||
				rec.Control != nil || len(rec.Errors) != 0 {
				t.Errorf("reason %q, control %v, errors %v", rec.Mutants[0].Reason, rec.Control, rec.Errors)
			}
		})
		t.Run("stops the runs when the caller's context ends before Admit admits them", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runIn(t, dir, run.Config{Admit: func(context.Context, int64) (func(), bool) { return nil, false }})
			want(
				t,
				verdicts(rec),
				"Add sbr-zero 0: not-run\nAdd aor 0: not-run\nSub sbr-zero 0: not-run\nSub aor 0: not-run\n"+
					"Unused sbr-zero 0: no-coverage\nUnused aor 0: no-coverage\n",
			)
			if rec.Mutants[0].Reason != "the caller cancelled the run" {
				t.Errorf("reason %q", rec.Mutants[0].Reason)
			}
		})
		t.Run("stops the runs when the caller cancels", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rec := runWith(t, ctx, dir, run.Config{Verdict: func(_ *record.Record, m record.Mutant) {
				if m.Verdict == record.Killed {
					cancel()
				}
			}})
			want(
				t,
				verdicts(rec),
				"Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: not-run\nSub sbr-zero 0: not-run\nSub aor 0: not-run\n"+
					"Unused sbr-zero 0: no-coverage\nUnused aor 0: no-coverage\n",
			)
			if rec.Mutants[1].Reason != "the caller cancelled the run" || rec.Control.Closing != nil {
				t.Errorf("reason %q, closing %v", rec.Mutants[1].Reason, rec.Control.Closing)
			}
			// The key of Add's aor, the least key of a mutant that did not run,
			// sorts after the key of Add's sbr-zero and before the keys of
			// Unused's mutants.
			if s := rec.Sample; s == nil || s.Before != rec.Mutants[1].Key || s.Detected != 1 || s.Undetected != 0 {
				t.Errorf("sample %+v, want the mutants before Add's aor: 1 detected", s)
			}
		})
		t.Run("gives error to a mutant whose run does not start", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			moved := dir + ".moved"
			var once, restore sync.Once
			rec := runIn(t, dir, run.Config{Verdict: func(_ *record.Record, m record.Mutant) {
				switch m.Verdict {
				case record.NoCoverage:
					once.Do(func() {
						if err := os.Rename(dir, moved); err != nil {
							t.Error(err)
						}
					})
				case record.Error:
					restore.Do(func() {
						if err := os.Rename(moved, dir); err != nil {
							t.Error(err)
						}
					})
				}
			}})
			want(
				t,
				verdicts(rec),
				"Add sbr-zero 0: error\nAdd aor 0: killed [TestAdd]\nSub sbr-zero 0: survived\nSub aor 0: survived\n"+
					"Unused sbr-zero 0: no-coverage\nUnused aor 0: no-coverage\n",
			)
			if !strings.HasPrefix(rec.Mutants[0].Reason, "the run did not start: ") || rec.Mutants[0].Seconds != nil {
				t.Errorf("reason %q, seconds %v", rec.Mutants[0].Reason, rec.Mutants[0].Seconds)
			}
		})
		t.Run("states ordinary-control-failed when a test fails in the ordinary build", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{
				"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
				"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
					"\tif os.Getenv(\"DOKIMI_MUTATE_MUTANT\") != \"\" && os.Getenv(\"DOKIMI_MUTATE_INSTRUMENTED\") == \"\" {\n" +
					"\t\tt.Fatal(\"a check of the ordinary build fails\")\n\t}\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
			})
			rec := runIn(t, dir, run.Config{Confirm: true})
			want(t, codes(rec), "ordinary-control-failed")
			want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail in an ordinary build with no mutant active: TestAdd failed\n",
			) || rec.Mutants[0].Reason != "the ordinary control run failed" || rec.Control.Ordinary != nil {
				t.Errorf("message %q, reason %q, ordinary %v", msg, rec.Mutants[0].Reason, rec.Control.Ordinary)
			}
		})
		t.Run("states the build error when the unchanged source does not build for the ordinary control run",
			func(t *testing.T) {
				t.Parallel()
				// The opening control run writes broken.go into the package
				// directory.
				write := "\t\t_ = os.WriteFile(\"broken.go\", []byte(" + strconv.Quote(broken) + "), 0o644)\n"
				dir := module(t, map[string]string{
					"add.go": "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
					"add_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
						"\tif os.Getenv(\"DOKIMI_MUTATE_TRACE\") != \"\" {\n" + write + "\t}\n" +
						"\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
				})
				rec := runIn(t, dir, run.Config{Confirm: true})
				want(t, codes(rec), "build changed-files")
				msg := rec.Errors[0].Message
				if !strings.HasPrefix(msg, "the ordinary build of the unchanged source fails: ") ||
					!strings.Contains(msg, "broken.go") || rec.Mutants[0].Reason != "the ordinary build failed" {
					t.Errorf("message %q, reason %q", msg, rec.Mutants[0].Reason)
				}
			},
		)
		t.Run("gives not-viable to a survivor whose ordinary build the toolchain rejects", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			var once sync.Once
			rec := runIn(t, dir, run.Config{Confirm: true, Verdict: func(_ *record.Record, m record.Mutant) {
				if m.Verdict == record.Killed {
					once.Do(func() {
						if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte(broken), 0o644); err != nil {
							t.Error(err)
						}
					})
				}
			}})
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n"+
				"Sub sbr-zero 0: not-viable confirmed\nSub aor 0: not-viable confirmed\n"+
				"Unused sbr-zero 0: not-viable confirmed\nUnused aor 0: not-viable confirmed\nchanged-files")
			if reason := rec.Mutants[2].Reason; !strings.Contains(reason, "broken.go") ||
				strings.HasPrefix(reason, "#") {
				t.Errorf("reason %q, want the toolchain's message", reason)
			}
		})
		t.Run(
			"starts no mutant when the time left is shorter than the deadlines and the builds that a confirmation needs",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and less than 17 are
				// left after the opening control run: twice the deadline, but not
				// three times.
				dir := module(t, map[string]string{
					"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
					"add_test.go": slowOpening,
				})
				rec := runIn(t, dir, run.Config{Confirm: true, Deadline: time.Now().Add(18 * time.Second)})
				want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
				if rec.Mutants[0].Reason != "the caller's deadline leaves too little time for the mutant, its confirmation and the closing control run" ||
					rec.Control.Ordinary == nil ||
					len(rec.Errors) != 0 {
					t.Errorf(
						"reason %q, ordinary %v, errors %v",
						rec.Mutants[0].Reason,
						rec.Control.Ordinary,
						rec.Errors,
					)
				}
			},
		)
		t.Run("runs no ordinary control run when the time left is shorter than its deadline and the closing run's",
			func(t *testing.T) {
				t.Parallel()
				// A mutant's deadline is above 7 seconds, and less than 10 are
				// left after the opening control run.
				dir := module(t, map[string]string{
					"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
					"add_test.go": slowOpening,
				})
				rec := runIn(t, dir, run.Config{Confirm: true, Deadline: time.Now().Add(10 * time.Second)})
				want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
				if rec.Mutants[0].Reason != "the caller's deadline leaves too little time for the ordinary control run" ||
					rec.Control.Ordinary != nil ||
					len(rec.Errors) != 0 {
					t.Errorf(
						"reason %q, ordinary %v, errors %v",
						rec.Mutants[0].Reason,
						rec.Control.Ordinary,
						rec.Errors,
					)
				}
			},
		)
		t.Run("returns an error when the package directory does not read", func(t *testing.T) {
			t.Parallel()
			_, err := run.Run(
				context.Background(),
				run.Config{Dir: filepath.Join(t.TempDir(), "missing"), Env: os.Environ()},
			)
			if err == nil || !strings.HasPrefix(err.Error(), "run: ") {
				t.Errorf("Run() error = %v", err)
			}
		})
	})
}

// TestRunTemporaryDirectory sets TMPDIR, the state of the whole process, so
// neither it nor its subtests run in parallel.
func TestRunTemporaryDirectory(t *testing.T) {
	t.Run("Run errors", func(t *testing.T) {
		t.Run("returns an error when the work directory cannot be made", func(t *testing.T) {
			t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			if _, err := run.Run(
				context.Background(),
				run.Config{Dir: ".", Env: os.Environ()},
			); err == nil ||
				!strings.HasPrefix(err.Error(), "run: ") {
				t.Errorf("Run() error = %v", err)
			}
		})
		t.Run("states closing-control-failed when the closing control run does not start", func(t *testing.T) {
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			base := t.TempDir()
			t.Setenv("TMPDIR", base)
			runs := 0
			rec := runIn(t, dir, run.Config{Verdict: func(_ *record.Record, m record.Mutant) {
				if m.Verdict != record.Killed && m.Verdict != record.Survived {
					return
				}
				if runs++; runs == 4 {
					bins, err := filepath.Glob(filepath.Join(base, "mutate-*", "pkg.test"))
					if err != nil || len(bins) != 1 {
						t.Errorf("test binaries %v, %v", bins, err)
					}
					for _, bin := range bins {
						if err := os.Remove(bin); err != nil {
							t.Error(err)
						}
					}
				}
			}})
			want(t, codes(rec), "closing-control-failed")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail with no mutant active after the mutant runs: the run did not start: ",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("gives error to a survivor whose ordinary build is not written", func(t *testing.T) {
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			base := t.TempDir()
			t.Setenv("TMPDIR", base)
			var once sync.Once
			// The files confirm2 and confirm3 take the places of the
			// directories of the confirmations of Sub's mutants, the record's
			// mutants 2 and 3.
			rec := runIn(t, dir, run.Config{Confirm: true, Verdict: func(_ *record.Record, m record.Mutant) {
				if m.Verdict != record.Killed {
					return
				}
				once.Do(func() {
					works, err := filepath.Glob(filepath.Join(base, "mutate-*"))
					if err != nil || len(works) != 1 {
						t.Errorf("work directories %v, %v", works, err)
					}
					for _, work := range works {
						for _, name := range []string{"confirm2", "confirm3"} {
							if err := os.WriteFile(filepath.Join(work, name), nil, 0o644); err != nil {
								t.Error(err)
							}
						}
					}
				})
			}})
			want(t, verdicts(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n"+
				"Sub sbr-zero 0: error confirmed\nSub aor 0: error confirmed\n"+
				"Unused sbr-zero 0: no-coverage confirmed\nUnused aor 0: no-coverage confirmed\n")
			if !strings.HasPrefix(rec.Mutants[2].Reason, "the ordinary build was not written: render: ") {
				t.Errorf("reason %q", rec.Mutants[2].Reason)
			}
		})
	})
}
