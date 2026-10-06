// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
)

// goLog is the directory of a go command that appends its GOMAXPROCS to the
// file that FIXTURE_GO_LOG names, and then runs the go command that
// FIXTURE_GO names. goBlock is the directory of a go command that runs the
// go command of FIXTURE_GO, except for a command with an argument that
// matches the shell pattern FIXTURE_BLOCK, which creates the file
// FIXTURE_MARKER and then waits a minute. TestMain writes both before any
// test starts, because a file open for writing while another goroutine
// forks a process fails exec with ETXTBSY.
var goLog, goBlock string

func TestMain(m *testing.M) {
	scripts := map[*string]string{
		&goLog: "#!/bin/sh\necho \"$GOMAXPROCS\" >> \"$FIXTURE_GO_LOG\"\nexec \"$FIXTURE_GO\" \"$@\"\n",
		&goBlock: "#!/bin/sh\nfor a in \"$@\"; do\n\tcase \"$a\" in\n\t$FIXTURE_BLOCK)\n" +
			"\t\t: > \"$FIXTURE_MARKER\"\n\t\texec sleep 60\n\t\t;;\n\tesac\ndone\nexec \"$FIXTURE_GO\" \"$@\"\n",
	}
	for dir, script := range scripts {
		var err error
		*dir, err = os.MkdirTemp("", "gowrapper")
		if err == nil {
			err = os.WriteFile(filepath.Join(*dir, "go"), []byte(script), 0o755)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(goLog)
	_ = os.RemoveAll(goBlock)
	os.Exit(code)
}

// procsTest is a test of a fixture with Add that fails unless its binary
// runs with GOMAXPROCS set to FIXTURE_PROCS.
const procsTest = "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
	"\tif os.Getenv(\"GOMAXPROCS\") != os.Getenv(\"FIXTURE_PROCS\") {\n\t\tt.Fatal(os.Getenv(\"GOMAXPROCS\"))\n\t}\n" +
	"\tif Add(2, 3) != 5 {\n\t\tt.Error(\"Add(2, 3) != 5\")\n\t}\n}\n"

// module writes files into a new directory, with a go.mod of the module
// fixture at go 1.21 unless files has one, and returns the directory.
func module(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module fixture\n\ngo 1.21\n"
	}
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runIn runs the engine on the package in dir with cfg, whose Dir it sets
// and whose Env it fills when it is nil.
func runIn(t *testing.T, dir string, cfg run.Config) *record.Record {
	t.Helper()
	return runWith(t, context.Background(), dir, cfg)
}

// runWith runs the engine as runIn does, with the context ctx.
func runWith(t *testing.T, ctx context.Context, dir string, cfg run.Config) *record.Record {
	t.Helper()
	cfg.Dir = dir
	if cfg.Env == nil {
		cfg.Env = os.Environ()
	}
	rec, err := run.Run(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// verdicts writes one line per mutant: its scope, its kind, its position
// among the mutants of its scope and kind, its verdict, the tests that it
// names, and confirmed for a confirmed mutant.
func verdicts(rec *record.Record) string {
	var b strings.Builder
	seen := map[string]int{}
	for _, m := range rec.Mutants {
		id := m.Scope + " " + m.Kind
		fmt.Fprintf(&b, "%s %d: %s", id, seen[id], m.Verdict)
		seen[id]++
		if len(m.Tests) > 0 {
			fmt.Fprintf(&b, " %v", m.Tests)
		}
		if m.Confirmed {
			b.WriteString(" confirmed")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// coveredBy writes one line per mutant: its scope, its kind, and the tests
// of its coveredBy, separated by commas.
func coveredBy(rec *record.Record) string {
	var b strings.Builder
	for _, m := range rec.Mutants {
		fmt.Fprintf(&b, "%s %s: %s\n", m.Scope, m.Kind, strings.Join(m.CoveredBy, ","))
	}
	return b.String()
}

// codes returns the codes of the record's run errors.
func codes(rec *record.Record) string {
	var list []string
	for _, e := range rec.Errors {
		list = append(list, e.Code)
	}
	return strings.Join(list, " ")
}

// want fails t when got differs from want.
func want(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// withoutKey returns env without the entries of key.
func withoutKey(env []string, key string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}
