// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package mutate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/mutate"
)

const arith = `package fixture

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

const arithTest = `package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	if os.Getenv("FIXTURE_FAIL") != "" {
		t.Fatal("FIXTURE_FAIL is set")
	}
	if os.Getenv("FIXTURE_SLOW") != "" && os.Getenv("DOKIMI_MUTATE_TRACE") != "" {
		time.Sleep(500 * time.Millisecond)
	}
	if want := os.Getenv("FIXTURE_PROCS"); want != "" && os.Getenv("GOMAXPROCS") != want {
		t.Fatal("GOMAXPROCS is " + os.Getenv("GOMAXPROCS"))
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}

func TestSub(t *testing.T) {
	if os.Getenv("FIXTURE_ORDINARY") != "" && os.Getenv("DOKIMI_MUTATE_INSTRUMENTED") == "" && Sub(5, 3) != 2 {
		t.Error("Sub(5, 3) != 2 in the ordinary build")
	}
	Sub(1, 1)
}
`

// linePrefix is what go test writes before the line of a mutant that Check
// reports in a top-level test: the indentation of the test's output.
const linePrefix = "    "

// seat records what Check reports to a test, and reports nothing to the
// test that it embeds.
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

func (*seat) Helper()             {}
func (s *seat) Error(args ...any) { s.errors = append(s.errors, fmt.Sprint(args...)) }
func (s *seat) Errorf(format string, a ...any) {
	s.errors = append(s.errors, fmt.Sprintf(format, a...))
}
func (s *seat) Fatalf(format string, a ...any) { s.fatal = fmt.Sprintf(format, a...) }
func (s *seat) Skip(args ...any)               { s.skipped = fmt.Sprint(args...) }
func (s *seat) Log(args ...any)                { s.logs = append(s.logs, fmt.Sprint(args...)) }
func (s *seat) Output() io.Writer              { return &s.output }
func (s *seat) Fail()                          { s.failed = true }

// Deadline returns the seat's deadline, and false for none.
func (s *seat) Deadline() (time.Time, bool) { return s.deadline, !s.deadline.IsZero() }

// failures returns the lines that fail the seat's test: the lines of its
// output, which fail it only together with Fail, and then its errors.
func failures(t *testing.T, s *seat) []string {
	t.Helper()
	var lines []string
	if s.output.Len() > 0 {
		if !s.failed {
			t.Errorf("Check wrote %q without failing the test", s.output.String())
		}
		lines = strings.Split(strings.TrimSuffix(s.output.String(), "\n"), "\n")
	}
	return append(lines, s.errors...)
}

// enter writes the fixture package into a new directory and makes it the
// working directory of the test process until t ends.
func enter(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{"go.mod": "module fixture\n\ngo 1.21\n", "arith.go": arith, "arith_test.go": arithTest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

// prefixes reports each of lines that does not start with the prefix at
// its index.
func prefixes(t *testing.T, lines, want []string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("got %d lines:\n%s\nwant %d", len(lines), strings.Join(lines, "\n"), len(want))
	}
	for i := range lines {
		if !strings.HasPrefix(lines[i], want[i]) {
			t.Errorf("line %d is %q, want the prefix %q", i, lines[i], want[i])
		}
	}
}

// TestCheck changes the working directory and the environment of the test
// process, so neither it nor its subtests run in parallel.
func TestCheck(t *testing.T) {
	survivors := []string{
		"arith.go:5:26: survived: return a - b became return 0 (sbr-zero)",
		"arith.go:5:33: survived: a - b became a + b (aor)",
	}
	uncovered := []string{
		"arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)",
		"arith.go:7:33: not covered: x * 2 became x / 2 (aor)",
	}
	t.Run("Check", func(t *testing.T) {
		t.Run("fails the test with one line for each undetected mutant at its position", func(t *testing.T) {
			enter(t)
			records := t.TempDir()
			t.Setenv("DOKIMI_MUTATE_RECORD_DIR", records)
			s := &seat{TB: t}
			mutate.Check(s, mutate.Option{})
			prefixes(t, failures(t, s), append(append([]string{}, survivors...), uncovered...))
			if s.fatal != "" || s.skipped != "" || len(s.logs) != 1 ||
				s.logs[0] != "mutate: fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered" {
				t.Errorf("fatal %q, skipped %q, logs %q", s.fatal, s.skipped, s.logs)
			}
			if _, err := os.Stat(filepath.Join(records, "fixture.mutate.json")); err != nil {
				t.Error(err)
			}
		})
		t.Run("runs the mutants on the workers that Workers states", func(t *testing.T) {
			enter(t)
			t.Setenv("FIXTURE_PROCS", strconv.Itoa(max(1, runtime.GOMAXPROCS(0)/2)))
			s := &seat{TB: t}
			mutate.Check(s, mutate.Workers(2))
			prefixes(t, failures(t, s), append(append([]string{}, survivors...), uncovered...))
		})
		t.Run("counts the tests of the packages that Suite names", func(t *testing.T) {
			dir := enter(t)
			check := map[string]string{
				"check.go": "package check\n",
				"check_test.go": "package check\n\nimport (\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n" +
					"func TestSub(t *testing.T) {\n\tif fixture.Sub(5, 3) != 2 {\n\t\tt.Error(\"Sub(5, 3) != 2\")\n\t}\n}\n",
			}
			if err := os.Mkdir(filepath.Join(dir, "check"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, text := range check {
				if err := os.WriteFile(filepath.Join(dir, "check", name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			s := &seat{TB: t}
			mutate.Check(s, mutate.Suite("./check"))
			prefixes(t, failures(t, s), uncovered)
		})
		t.Run("confirms each survivor in its ordinary build under Confirm", func(t *testing.T) {
			// TestSub checks Sub only in an ordinary build, so the
			// confirmation kills both of Sub's survivors.
			enter(t)
			t.Setenv("FIXTURE_ORDINARY", "1")
			s := &seat{TB: t}
			mutate.Check(s, mutate.Confirm())
			prefixes(t, failures(t, s), uncovered)
		})
		t.Run("mutates the generated files under IncludeGenerated", func(t *testing.T) {
			dir := enter(t)
			generated := "// Code generated by hand. DO NOT EDIT.\n\npackage fixture\n\nfunc Double(x int) int { return x * 2 }\n"
			if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte(generated), 0o644); err != nil {
				t.Fatal(err)
			}
			s := &seat{TB: t}
			mutate.Check(s, mutate.IncludeGenerated())
			prefixes(t, failures(t, s), append(append(append([]string{}, survivors...), uncovered...),
				"gen.go:5:26: not covered: return x * 2 became return 0 (sbr-zero)",
				"gen.go:5:33: not covered: x * 2 became x / 2 (aor)"))
			want := "mutate: fixture: 2 of 8 mutants detected (25%): 2 killed, 2 survived, 4 not covered, " +
				"1 generated file with 2 mutants included"
			if len(s.logs) != 1 || s.logs[0] != want {
				t.Errorf("logs %q, want %q", s.logs, want)
			}
		})
		t.Run("restricts the run to the lines of DOKIMI_MUTATE_LINES", func(t *testing.T) {
			enter(t)
			t.Setenv("DOKIMI_MUTATE_LINES", "arith.go:5-5,arith.go:6-6")
			s := &seat{TB: t}
			mutate.Check(s)
			prefixes(t, failures(t, s), survivors)
		})
		t.Run("passes the test's deadline to the run", func(t *testing.T) {
			// The opening control run sleeps half a second, so a mutant's
			// deadline is above 7 seconds, and less than 10 are left.
			enter(t)
			t.Setenv("FIXTURE_SLOW", "1")
			s := &seat{TB: t, deadline: time.Now().Add(10 * time.Second)}
			mutate.Check(s)
			prefixes(
				t,
				failures(t, s),
				append(
					append([]string{}, uncovered...),
					"mutate: 4 mutants did not run: the caller's deadline leaves too little time",
				),
			)
		})
		t.Run("fails the test with each run error and the mutants that did not run", func(t *testing.T) {
			enter(t)
			t.Setenv("FIXTURE_FAIL", "1")
			s := &seat{TB: t}
			mutate.Check(s)
			prefixes(t, failures(t, s), []string{
				"mutate: control-failed: the tests fail with no mutant active: TestAdd failed",
				"mutate: 6 mutants did not run: the opening control run failed",
			})
		})
		t.Run("skips the test in a binary that Check started", func(t *testing.T) {
			t.Setenv("DOKIMI_MUTATE_MUTANT", "3")
			s := &seat{TB: t}
			mutate.Check(s)
			if s.skipped != "mutate: Check does not run in a test binary that Check started" || len(s.errors) != 0 ||
				len(s.logs) != 0 {
				t.Errorf("skipped %q, errors %q, logs %q", s.skipped, s.errors, s.logs)
			}
		})
		t.Run("stops the test for a selection that does not parse", func(t *testing.T) {
			t.Setenv("DOKIMI_MUTATE_LINES", "arith.go:5-5,arith.go")
			s := &seat{TB: t}
			mutate.Check(s)
			if !strings.HasPrefix(s.fatal, "mutate: DOKIMI_MUTATE_LINES: selection: ") || len(s.logs) != 0 {
				t.Errorf("fatal %q, logs %q", s.fatal, s.logs)
			}
		})
		t.Run("stops the test when the run cannot make its work directory", func(t *testing.T) {
			enter(t)
			t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
			s := &seat{TB: t}
			mutate.Check(s)
			if !strings.HasPrefix(s.fatal, "mutate: run: ") || len(s.logs) != 0 {
				t.Errorf("fatal %q, logs %q", s.fatal, s.logs)
			}
		})
		t.Run("fails the test when the record cannot be written", func(t *testing.T) {
			dir := enter(t)
			t.Setenv("DOKIMI_MUTATE_RECORD_DIR", filepath.Join(dir, "arith.go"))
			s := &seat{TB: t}
			mutate.Check(s)
			if lines := failures(t, s); !strings.HasPrefix(lines[len(lines)-1], "mutate: record: ") {
				t.Errorf("the last failure is %q, want the record's", lines[len(lines)-1])
			}
		})
	})
}

func TestWorkers(t *testing.T) {
	t.Parallel()
	t.Run("Workers", func(t *testing.T) {
		t.Parallel()
		t.Run("panics for fewer than one worker", func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != "mutate: Workers(0) states fewer than one worker" {
					t.Errorf("Workers(0) panics with %v", r)
				}
			}()
			mutate.Workers(0)
		})
	})
}

// TestModule runs Check the way a module that requires this one runs it:
// from go test, behind a build constraint.
func TestModule(t *testing.T) {
	t.Parallel()
	t.Run("Check", func(t *testing.T) {
		t.Parallel()
		t.Run("fails the test that calls it for each survivor and writes the record", func(t *testing.T) {
			t.Parallel()
			root, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			// The fixture requires this module, so it needs this module's go
			// line and the checksums of this module's requirements.
			sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			files := map[string]string{
				"go.mod": "module fixture\n\ngo 1.27.0\n\nrequire go.dokimi.dev/mutate v0.0.0\n\n" +
					"replace go.dokimi.dev/mutate => " + root + "\n",
				"go.sum":        string(sums),
				"arith.go":      arith,
				"arith_test.go": arithTest,
				"mutation_test.go": "//go:build mutation\n\npackage fixture\n\nimport (\n\t\"testing\"\n\n\t\"go.dokimi.dev/mutate\"\n)\n\n" +
					"func TestMutation(t *testing.T) {\n\tmutate.Check(t)\n}\n",
			}
			for name, text := range files {
				if err = os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			records := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(
				ctx,
				"go",
				"test",
				"-tags",
				"mutation",
				"-run",
				"^TestMutation$",
				"-count=1",
				"-timeout",
				"0",
				".",
			)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "DOKIMI_MUTATE_RECORD_DIR="+records)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("go test: %v\n%s", err, out)
			}
			for _, want := range []string{
				"--- FAIL: TestMutation",
				"\n" + linePrefix + "arith.go:5:26: survived: return a - b became return 0 (sbr-zero)\n",
				"\n" + linePrefix + "arith.go:7:33: not covered: x * 2 became x / 2 (aor)\n",
				"mutate: fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered\n",
			} {
				if !strings.Contains(string(out), want) {
					t.Errorf("the output lacks %q:\n%s", want, out)
				}
			}
			if _, err := os.Stat(filepath.Join(records, "fixture.mutate.json")); err != nil {
				t.Error(err)
			}
		})
	})
}
