// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/mutate/internal/record"
)

// mainVar is the variable of the environment that makes the test binary
// run main in place of its tests. go test merges the coverage of that
// process into the profile.
const mainVar = "MUTATE_TEST_MAIN"

// fakeGoDir contains a go command that fails for go list when FAKE_GO is
// fail, and prints text that is no JSON otherwise. TestMain writes it
// before any test starts, because a file open for writing while another
// goroutine forks a process fails exec with ETXTBSY.
var fakeGoDir string

func TestMain(m *testing.M) {
	if os.Getenv(mainVar) != "" {
		main()
	}
	dir, err := os.MkdirTemp("", "fakego")
	if err == nil {
		fakeGoDir = dir
		script := "#!/bin/sh\nif [ \"$FAKE_GO\" = fail ]; then echo \"no list today\" >&2; exit 1; fi\necho \"{not json\"\n"
		err = os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

const arith = `package fixture

func Add(a, b int) int { return a + b }

func Sub(a, b int) int { return a - b }

func Unused(x int) int { return x * 2 }
`

const arithTest = `package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if os.Getenv("FIXTURE_FAIL") != "" {
		t.Fatal("FIXTURE_FAIL is set")
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

const add = "package fixture\n\nfunc Add(a, b int) int { return a + b }\n"

const addTest = "package fixture\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n"

// enter writes files, by slash-separated path, and a go.mod of the module
// fixture into a new directory, and makes it the working directory of the
// test process until t ends.
func enter(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module fixture\n\ngo 1.21\n"
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Error(err)
		}
	})
	return dir
}

// call runs the command line args and returns its exit status, its
// standard output and its standard error. It writes no progress line.
func call(args ...string) (status int, stdout, stderr string) {
	var out, errs bytes.Buffer
	p := &printer{stdout: &out, stderr: &errs, every: time.Hour, now: time.Now}
	status = p.mutate(context.Background(), args)
	return status, out.String(), errs.String()
}

// cancelling is a standard output that cancels the command's context at its
// first write.
type cancelling struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (c *cancelling) Write(data []byte) (int, error) {
	c.cancel()
	return c.Buffer.Write(data)
}

// arithLines is what mutate prints for the fixture arith: the mutants
// without coverage, whose verdicts are final when the opening control run
// ends, then the survivors in the order of their keys, and the summary.
const arithLines = `arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)
arith.go:7:33: not covered: x * 2 became x / 2 (aor)
arith.go:5:26: survived: return a - b became return 0 (sbr-zero)
arith.go:5:33: survived: a - b became a + b (aor)
fixture: 2 of 6 mutants detected (33%): 2 killed, 2 survived, 2 not covered
`

// procsTest is a test of the fixture add that fails unless its binary runs
// with GOMAXPROCS set to FIXTURE_PROCS.
const procsTest = "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
	"\tif os.Getenv(\"GOMAXPROCS\") != os.Getenv(\"FIXTURE_PROCS\") {\n\t\tt.Fatal(\"GOMAXPROCS is \" + os.Getenv(\"GOMAXPROCS\"))\n\t}\n" +
	"\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n"

// TestMutate changes the working directory and the environment of the test
// process, so neither it nor its subtests run in parallel.
func TestMutate(t *testing.T) {
	t.Run("mutate", func(t *testing.T) {
		t.Run("prints the line of each undetected mutant when its verdict is final", func(t *testing.T) {
			enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			if status, stdout, stderr := call(); status != 1 || stdout != arithLines || stderr != "" {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant:\n%s", status, stderr, stdout, arithLines)
			}
		})
		t.Run("exits 0 when the tests detect every mutant", func(t *testing.T) {
			enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
			status, stdout, stderr := call("-workers", "2")
			if status != 0 || stdout != "fixture: 2 of 2 mutants detected (100%): 2 killed\n" || stderr != "" {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
			}
		})
		t.Run("confirms each survivor in its ordinary build under -confirm", func(t *testing.T) {
			// TestSub checks Sub only in an ordinary build, so the
			// confirmation kills both of Sub's survivors.
			enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			t.Setenv("FIXTURE_ORDINARY", "1")
			status, stdout, stderr := call("-confirm")
			want := "arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)\n" +
				"arith.go:7:33: not covered: x * 2 became x / 2 (aor)\n" +
				"fixture: 4 of 6 mutants detected (66%): 4 killed, 2 not covered\n"
			if status != 1 || stdout != want || stderr != "" {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant:\n%s", status, stderr, stdout, want)
			}
		})
		t.Run("restricts the run to the lines of -lines", func(t *testing.T) {
			enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			status, stdout, _ := call("-lines", "arith.go:5-5", "-lines", "arith.go:6-6")
			want := "arith.go:5:26: survived: return a - b became return 0 (sbr-zero)\n" +
				"arith.go:5:33: survived: a - b became a + b (aor)\n" +
				"fixture: 0 of 2 mutants detected (0%): 2 survived, 4 not selected\n"
			if status != 1 || stdout != want {
				t.Errorf("status %d, stdout:\n%s\nwant:\n%s", status, stdout, want)
			}
		})
		t.Run("restricts the run to the lines that the diff of -diff changes", func(t *testing.T) {
			dir := enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			diff := "--- a/arith.go\n+++ b/arith.go\n@@ -5 +5 @@\n" +
				"-func Sub(a, b int) int { return b - a }\n+func Sub(a, b int) int { return a - b }\n"
			if err := os.WriteFile(filepath.Join(dir, "change.diff"), []byte(diff), 0o644); err != nil {
				t.Fatal(err)
			}
			status, stdout, stderr := call("-diff", "change.diff")
			want := "arith.go:5:26: survived: return a - b became return 0 (sbr-zero)\n" +
				"arith.go:5:33: survived: a - b became a + b (aor)\n" +
				"fixture: 0 of 2 mutants detected (0%): 2 survived, 4 not selected\n"
			if status != 1 || stdout != want || stderr != "" {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant:\n%s", status, stderr, stdout, want)
			}
		})
		t.Run("selects no mutant for an empty diff on standard input under -diff -", func(t *testing.T) {
			enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			var out, errs bytes.Buffer
			p := &printer{stdin: strings.NewReader(""), stdout: &out, stderr: &errs, every: time.Hour, now: time.Now}
			if status := p.mutate(context.Background(), []string{"-diff", "-"}); status != 0 ||
				out.String() != "fixture: no mutant to detect: 6 not selected\n" || errs.String() != "" {
				t.Errorf("status %d, stdout %q, stderr %q", status, out.String(), errs.String())
			}
		})
		t.Run("exits 2 when the diff of -diff does not read or does not match the files", func(t *testing.T) {
			dir := enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			stale := "+++ b/arith.go\n@@ -5 +5 @@\n-func Sub(a, b int) int { return a - b }\n+func Sub(a, b int) int { return b - a }\n"
			if err := os.WriteFile(filepath.Join(dir, "stale.diff"), []byte(stale), 0o644); err != nil {
				t.Fatal(err)
			}
			// The diff's paths are relative to the working directory, as
			// filepath.Abs resolves them.
			arithPath, err := filepath.Abs("arith.go")
			if err != nil {
				t.Fatal(err)
			}
			tests := []struct{ give, want string }{
				{"missing.diff", name + ": -diff: open missing.diff: no such file or directory\n"},
				{"stale.diff", name + ": -diff: run: the diff states line 5 of " + arithPath +
					" otherwise than the file, so it is out of date\n"},
			}
			for _, tt := range tests {
				if status, stdout, stderr := call("-diff", tt.give); status != 2 || stdout != "" || stderr != tt.want {
					t.Errorf(
						"-diff %s: status %d, stdout %q, stderr %q, want %q",
						tt.give,
						status,
						stdout,
						stderr,
						tt.want,
					)
				}
			}
		})
		t.Run("counts the tests of the packages of -suite, relative to the working directory", func(t *testing.T) {
			dir := enter(t, map[string]string{
				"arith.go":       arith,
				"arith_test.go":  arithTest,
				"check/check.go": "package check\n",
				"check/check_test.go": "package check\n\nimport (\n\t\"testing\"\n\n\t\"fixture\"\n)\n\n" +
					"func TestSub(t *testing.T) {\n\tif fixture.Sub(5, 3) != 2 {\n\t\tt.Error(\"Sub(5, 3) != 2\")\n\t}\n}\n",
			})
			// The working directory is a symbolic link to the module, as
			// PWD states it, and each run resolves the suite in the
			// package's directory, which go list states without the link.
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(dir, link); err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(link); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PWD", link)
			status, stdout, stderr := call("-suite", "./check", "-suite", "fixture/missing/...")
			want := "arith.go:7:26: not covered: return x * 2 became return 0 (sbr-zero)\n" +
				"arith.go:7:33: not covered: x * 2 became x / 2 (aor)\n" +
				"fixture: 4 of 6 mutants detected (66%): 4 killed, 2 not covered\n"
			if status != 1 || stdout != want {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant:\n%s", status, stderr, stdout, want)
			}
		})
		t.Run("checks every package that the patterns name and writes their records", func(t *testing.T) {
			enter(
				t,
				map[string]string{
					"add.go":          add,
					"add_test.go":     addTest,
					"sub/add.go":      strings.Replace(add, "fixture", "sub", 1),
					"sub/add_test.go": strings.Replace(addTest, "fixture", "sub", 1),
				},
			)
			records := t.TempDir()
			status, stdout, stderr := call("-p", "2", "-record", records, "./...")
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			sort.Strings(lines)
			want := "fixture/sub: 2 of 2 mutants detected (100%): 2 killed\nfixture: 2 of 2 mutants detected (100%): 2 killed"
			if got := strings.Join(lines, "\n"); status != 0 || stderr != "" || got != want {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant the lines:\n%s", status, stderr, stdout, want)
			}
			for _, name := range []string{"fixture.mutate.json", "fixture%2Fsub.mutate.json"} {
				if _, err := os.Stat(filepath.Join(records, name)); err != nil {
					t.Error(err)
				}
			}
		})
		t.Run("runs the packages within the memory of -memory", func(t *testing.T) {
			// One byte admits the runs of one package at a time, and 0 sets no
			// limit. Both runs check both packages.
			for _, memory := range []string{"1", "0"} {
				enter(t, map[string]string{
					"add.go":          add,
					"add_test.go":     addTest,
					"sub/add.go":      strings.Replace(add, "fixture", "sub", 1),
					"sub/add_test.go": strings.Replace(addTest, "fixture", "sub", 1),
				})
				status, stdout, stderr := call("-p", "2", "-memory", memory, "./...")
				lines := strings.Split(strings.TrimSpace(stdout), "\n")
				sort.Strings(lines)
				want := "fixture/sub: 2 of 2 mutants detected (100%): 2 killed\nfixture: 2 of 2 mutants detected (100%): 2 killed"
				if got := strings.Join(lines, "\n"); status != 0 || stderr != "" || got != want {
					t.Errorf("-memory %s: status %d, stderr %q, stdout:\n%s", memory, status, stderr, stdout)
				}
			}
		})
		t.Run("starts the runs of the first mutants in key order under -sample", func(t *testing.T) {
			enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			status, stdout, stderr := call("-sample", "2")
			stopped := name + ": fixture: 2 mutants did not run: the caller limits the run to 2 mutants\n"
			if status != 2 || !strings.HasPrefix(stdout, "arith.go:7:26: not covered:") ||
				!strings.Contains(stdout, "fixture: the run failed, ") || !strings.HasSuffix(stderr, stopped) {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
			}
		})
		t.Run("divides GOMAXPROCS among the packages and then among the workers", func(t *testing.T) {
			enter(t, map[string]string{
				"add.go":          add,
				"add_test.go":     procsTest,
				"sub/add.go":      strings.Replace(add, "fixture", "sub", 1),
				"sub/add_test.go": strings.Replace(procsTest, "fixture", "sub", 1),
			})
			t.Setenv("FIXTURE_PROCS", strconv.Itoa(max(1, max(1, runtime.GOMAXPROCS(0)/2)/2)))
			if status, stdout, stderr := call("-p", "2", "-workers", "2", "./..."); status != 0 || stderr != "" {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
			}
		})
		t.Run("writes each package's record as one line of JSON under -json", func(t *testing.T) {
			enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
			status, stdout, stderr := call("-json")
			var rec record.Record
			if err := json.Unmarshal([]byte(stdout), &rec); err != nil {
				t.Fatalf("stdout %q: %v", stdout, err)
			}
			if status != 0 || stderr != "" || strings.Count(stdout, "\n") != 1 || rec.Target.Name != "fixture" ||
				len(rec.Mutants) != 2 || rec.Score == nil || *rec.Score != 1 {
				t.Errorf("status %d, stderr %q, stdout %q", status, stderr, stdout)
			}
		})
		t.Run(
			"states the run error and the mutants that did not run of a run that fails, and exits 2",
			func(t *testing.T) {
				enter(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
				t.Setenv("FIXTURE_FAIL", "1")
				status, stdout, stderr := call()
				if status != 2 || stdout != "fixture: the run failed: 6 not run\n" ||
					!strings.HasPrefix(
						stderr,
						name+": fixture: control-failed: the tests fail with no mutant active: TestAdd failed\n",
					) ||
					!strings.HasSuffix(
						stderr,
						"\n"+name+": fixture: 6 mutants did not run: the opening control run failed\n",
					) {
					t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
				}
			},
		)
		t.Run("starts no package after -timeout", func(t *testing.T) {
			enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
			status, stdout, stderr := call("-timeout", "1ns")
			if status != 2 || stdout != "" ||
				stderr != name+": fixture: not checked, because -timeout passed before the package started\n" {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
			}
		})
		t.Run("starts no mutant that would not end before -timeout", func(t *testing.T) {
			enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
			status, stdout, stderr := call("-timeout", "3s")
			if status != 2 || stdout != "fixture: the run failed: 2 not run\n" || !strings.HasPrefix(
				stderr,
				name+": fixture: 2 mutants did not run: the caller's deadline leaves too little time for the ",
			) {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout, stderr)
			}
		})
		t.Run("starts no package after an interrupt", func(t *testing.T) {
			enter(t, map[string]string{
				"add.go":          add,
				"add_test.go":     addTest,
				"sub/add.go":      strings.Replace(add, "fixture", "sub", 1),
				"sub/add_test.go": strings.Replace(addTest, "fixture", "sub", 1),
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := &cancelling{cancel: cancel}
			var errs bytes.Buffer
			p := &printer{stdout: out, stderr: &errs, every: time.Hour, now: time.Now}
			if status := p.mutate(ctx, []string{"./..."}); status != 2 ||
				out.String() != "fixture: 2 of 2 mutants detected (100%): 2 killed\n" ||
				errs.String() != name+": fixture/sub: not checked, because the command was interrupted\n" {
				t.Errorf("status %d, stdout %q, stderr %q", status, out.String(), errs.String())
			}
		})
		t.Run("writes the progress of each package that runs", func(t *testing.T) {
			// Each run of the test takes 200 ms, so a progress line every 10 ms
			// falls in the run of the second mutant.
			enter(t, map[string]string{
				"add.go": add,
				"add_test.go": "package fixture\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
					"\ttime.Sleep(200 * time.Millisecond)\n\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n",
			})
			var out, errs bytes.Buffer
			p := &printer{stdout: &out, stderr: &errs, every: 10 * time.Millisecond, now: time.Now}
			status := p.mutate(context.Background(), nil)
			lines := strings.Split(strings.TrimSuffix(errs.String(), "\n"), "\n")
			progress := regexp.MustCompile(
				`^` + name + `: fixture: (running|[0-2] of 2 mutants done, 0 undetected, [0-9]+\.[0-9] mutants a second, about [0-9hms.]+ left)$`,
			)
			for _, line := range lines {
				if !progress.MatchString(line) {
					t.Errorf("stderr has the line %q, want only progress lines", line)
				}
			}
			if status != 0 || lines[0] != name+": fixture: running" ||
				!strings.Contains(errs.String(), "\n"+name+": fixture: 1 of 2 mutants done, 0 undetected, ") {
				t.Errorf("status %d, stderr %q", status, errs.String())
			}
		})
	})
	t.Run("mutate errors", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T) []string
			want  string
		}{
			{"refuses a flag that it does not know", func(t *testing.T) []string {
				t.Helper()
				return []string{"-unknown"}
			}, "flag provided but not defined: -unknown"},
			{"refuses a selection that does not parse", func(t *testing.T) []string {
				t.Helper()
				return []string{"-lines", "a.go"}
			}, `invalid value "a.go" for flag -lines`},
			{"refuses fewer than one package at once", func(t *testing.T) []string {
				t.Helper()
				return []string{"-p", "0"}
			}, name + ": -p and -workers take a number of at least 1"},
			{"refuses fewer than one worker", func(t *testing.T) []string {
				t.Helper()
				return []string{"-workers", "0"}
			}, name + ": -p and -workers take a number of at least 1"},
			{"refuses a negative timeout", func(t *testing.T) []string {
				t.Helper()
				return []string{"-timeout", "-1s"}
			}, name + ": -timeout takes a duration of at least 0"},
			{"refuses a negative sample", func(t *testing.T) []string {
				t.Helper()
				return []string{"-sample", "-1"}
			}, name + ": -sample takes a number of at least 0"},
			{"refuses a memory that is no number of bytes", func(t *testing.T) []string {
				t.Helper()
				return []string{"-memory", "8E"}
			}, `invalid value "8E" for flag -memory`},
			{"states a pattern that names no package", func(t *testing.T) []string {
				t.Helper()
				enter(t, map[string]string{"add.go": add})
				return []string{"./missing"}
			}, name + ": ./missing: stat "},
			{"states a package that does not load by its import path", func(t *testing.T) []string {
				t.Helper()
				enter(t, map[string]string{"add.go": add, "broken/b.go": "package broken\n\nfunc {\n"})
				return []string{"./broken"}
			}, name + ": fixture/broken: load: # fixture/broken\n"},
			{"states a pattern of -suite that names no package", func(t *testing.T) []string {
				t.Helper()
				enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
				return []string{"-suite", "./missing"}
			}, name + ": -suite: ./missing: stat "},
			{"states a package of -suite whose dependency does not resolve", func(t *testing.T) []string {
				t.Helper()
				enter(t, map[string]string{
					"add.go":         add,
					"add_test.go":    addTest,
					"check/check.go": "package check\n\nimport _ \"fixture/missing\"\n",
				})
				return []string{"-suite", "./check"}
			}, name + ": -suite: fixture/check: package fixture/missing is not in "},
			{"states a go list of -suite that fails", func(t *testing.T) []string {
				t.Helper()
				t.Setenv("PATH", fakeGoDir+string(filepath.ListSeparator)+os.Getenv("PATH"))
				t.Setenv("FAKE_GO", "fail")
				return []string{"-suite", "./check"}
			}, name + ": -suite: go list -e -json=Dir,ImportPath,Error,DepsErrors ./check: exit status 1\nno list today"},
			{"states a go list that fails", func(t *testing.T) []string {
				t.Helper()
				t.Setenv("PATH", fakeGoDir+string(filepath.ListSeparator)+os.Getenv("PATH"))
				t.Setenv("FAKE_GO", "fail")
				return nil
			}, name + ": go list -e -json=Dir,ImportPath,Error,DepsErrors .: exit status 1\nno list today"},
			{"states a go list that prints no JSON", func(t *testing.T) []string {
				t.Helper()
				t.Setenv("PATH", fakeGoDir+string(filepath.ListSeparator)+os.Getenv("PATH"))
				return nil
			}, name + ": go list: invalid character"},
			{"states a run that cannot make its work directory", func(t *testing.T) []string {
				t.Helper()
				enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
				t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
				return nil
			}, name + ": fixture: run: "},
			{"states a record that cannot be written", func(t *testing.T) []string {
				t.Helper()
				dir := enter(t, map[string]string{"add.go": add, "add_test.go": addTest})
				return []string{"-record", filepath.Join(dir, "add.go")}
			}, name + ": record: "},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				status, _, stderr := call(tt.setup(t)...)
				if status != 2 || !strings.Contains(stderr, tt.want) {
					t.Errorf("status %d, stderr %q, want 2 and %q", status, stderr, tt.want)
				}
			})
		}
	})
}

func TestCommand(t *testing.T) {
	t.Parallel()
	t.Run("main", func(t *testing.T) {
		t.Parallel()
		t.Run("exits with the status of the command line of the process", func(t *testing.T) {
			t.Parallel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-p", "0")
			cmd.Env = append(os.Environ(), mainVar+"=1")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err = cmd.Run()
			var exit *exec.ExitError
			// The spelling of the command's name is pinned here once.
			if !errors.As(err, &exit) || exit.ExitCode() != 2 ||
				stderr.String() != "dokimi-mutate-go: -p and -workers take a number of at least 1\n" {
				t.Errorf("error %v, stderr %q", err, stderr.String())
			}
		})
	})
	t.Run("printer", func(t *testing.T) {
		t.Parallel()
		t.Run("verdict", func(t *testing.T) {
			t.Parallel()
			wd, err := os.Getwd()
			if err == nil {
				wd, err = filepath.EvalSymlinks(wd)
			}
			if err != nil {
				t.Fatal(err)
			}
			mutant := record.Mutant{
				Kind: "ror-boundary", File: "a.go", Start: record.Position{Line: 7, Column: 5},
				Original: "a < b", Replacement: "a <= b",
			}
			tests := []struct {
				name           string
				verdict, cause string
				lines          bool
				want           string
			}{
				{
					"writes the line of a survivor",
					record.Survived, "", true,
					"a.go:7:5: survived: a < b became a <= b (ror-boundary)\n",
				},
				{
					"writes the line of a mutant without coverage",
					record.NoCoverage, "", true,
					"a.go:7:5: not covered: a < b became a <= b (ror-boundary)\n",
				},
				{
					"writes the line of a mutant whose run ended in an error",
					record.Error, "the run did not start", true,
					"a.go:7:5: error: a < b became a <= b (ror-boundary): the run did not start\n",
				},
				{"writes no line of a killed mutant", record.Killed, "", true, ""},
				{"writes no line without the text output", record.Survived, "", false, ""},
			}
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					var out bytes.Buffer
					p := &printer{stdout: &out, stderr: io.Discard, now: time.Now, running: map[string]*progress{}}
					p.begin("fixture")
					m := mutant
					m.Verdict, m.Reason = tt.verdict, tt.cause
					p.verdict("fixture", &record.Record{Root: wd, Mutants: []record.Mutant{m}}, m, tt.lines)
					if out.String() != tt.want {
						t.Errorf("stdout %q, want %q", out.String(), tt.want)
					}
				})
			}
		})
		t.Run("tick", func(t *testing.T) {
			t.Parallel()
			start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
			// Each mutant that ran took 2 seconds, and tick reads the clock 2
			// seconds after the verdicts: 2 runs in 4 seconds.
			seconds := 2.0
			ran := []record.Mutant{
				{Verdict: record.Killed, Seconds: &seconds},
				{Verdict: record.Survived, Seconds: &seconds},
			}
			// stopped returns the mutants of ran and n mutants that do not run.
			stopped := func(n int) []record.Mutant {
				out := append([]record.Mutant{}, ran...)
				for i := 0; i < n; i++ {
					out = append(out, record.Mutant{Verdict: record.NotRun})
				}
				return out
			}
			tests := []struct {
				name     string
				verdicts []record.Mutant
				deadline time.Time
				want     string
			}{
				{"states that a package runs before its first verdict", nil, time.Time{}, "running"},
				{
					"states the mutants done and the undetected ones among them",
					[]record.Mutant{{Verdict: record.NoCoverage}, {Verdict: record.Suppressed}},
					time.Time{},
					"2 of 10 mutants done, 1 undetected",
				},
				{
					"states the rate of the runs and the time left at that rate",
					ran,
					time.Time{},
					"2 of 10 mutants done, 1 undetected, 0.5 mutants a second, about 16s left",
				},
				{
					"states the mutants that do not run and the mutant runs that the run waits for",
					stopped(5),
					time.Time{},
					"2 of 10 mutants done, 1 undetected, 5 not run, waiting for 3 mutant runs and the closing control run",
				},
				{
					"states one mutant run that the run waits for in the singular",
					stopped(7),
					time.Time{},
					"2 of 10 mutants done, 1 undetected, 7 not run, waiting for 1 mutant run and the closing control run",
				},
				{
					"states that the run waits for the closing control run once each mutant has a verdict",
					stopped(8),
					time.Time{},
					"2 of 10 mutants done, 1 undetected, 8 not run, waiting for the closing control run",
				},
				{"states the time until -timeout", nil, start.Add(92 * time.Second), "running, -timeout in 1m30s"},
				{"states that -timeout passed", nil, start.Add(-time.Second), "running, -timeout passed"},
			}
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					var errs bytes.Buffer
					now := start
					p := &printer{
						stdout:   io.Discard,
						stderr:   &errs,
						now:      func() time.Time { return now },
						deadline: tt.deadline,
						running:  map[string]*progress{},
					}
					p.begin("fixture")
					rec := &record.Record{Mutants: make([]record.Mutant, 10)}
					for _, m := range tt.verdicts {
						p.verdict("fixture", rec, m, false)
					}
					now = now.Add(2 * time.Second)
					p.tick()
					if want := name + ": fixture: " + tt.want + "\n"; errs.String() != want {
						t.Errorf("stderr %q, want %q", errs.String(), want)
					}
				})
			}
			t.Run("writes the line of each package that runs in import path order", func(t *testing.T) {
				t.Parallel()
				var errs bytes.Buffer
				p := &printer{stdout: io.Discard, stderr: &errs, now: time.Now, running: map[string]*progress{}}
				for _, pkg := range []string{"c", "a", "b"} {
					p.begin(pkg)
				}
				p.end("c")
				p.tick()
				if want := name + ": a: running\n" + name + ": b: running\n"; errs.String() != want {
					t.Errorf("stderr %q, want %q", errs.String(), want)
				}
			})
		})
	})
}
