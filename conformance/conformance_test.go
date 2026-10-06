// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
)

// corpus is the directory of the vendored corpus.
var corpus = filepath.Join("spec", "corpus")

// caseFile is a case's case.json: the mutants and the run errors of the
// case in every language, and whether its run confirms its survivors.
type caseFile struct {
	Case    string       `json:"case"`
	Confirm bool         `json:"confirm"`
	Proves  string       `json:"proves"`
	Fixture string       `json:"fixture"`
	Mutants []caseMutant `json:"mutants"`
	Errors  []string     `json:"errors"`
}

// caseMutant is a mutant of case.json, which nth identifies among the
// mutants of its scope and kind.
type caseMutant struct {
	Scope     string   `json:"scope"`
	Kind      string   `json:"kind"`
	Nth       int      `json:"nth"`
	Verdict   string   `json:"verdict"`
	Confirmed bool     `json:"confirmed"`
	Languages []string `json:"languages"`
}

// expectFile is a Go fixture's expect.json: what the record states beyond
// the verdicts.
type expectFile struct {
	Language  string             `json:"language"`
	Lines     []string           `json:"lines"`
	Suite     []string           `json:"suite"`
	Mutants   []expectMutant     `json:"mutants"`
	Skipped   []record.Skip      `json:"skipped"`
	Generated []record.Generated `json:"generated"`
	Errors    []string           `json:"errors"`
}

// expectMutant is a mutant of expect.json.
type expectMutant struct {
	Scope       string          `json:"scope"`
	Kind        string          `json:"kind"`
	Nth         int             `json:"nth"`
	Occurrence  int             `json:"occurrence"`
	Key         string          `json:"key"`
	File        string          `json:"file"`
	Start       record.Position `json:"start"`
	End         record.Position `json:"end"`
	Original    string          `json:"original"`
	Replacement string          `json:"replacement"`
	Rule        string          `json:"rule"`
	Reason      string          `json:"reason"`
	NotViable   bool            `json:"notViable"`
	Tests       []string        `json:"tests"`
	CoveredBy   []string        `json:"coveredBy"`
}

// decode reads the JSON file at path into v, and fails t when the file
// names a field that v's type lacks.
func decode(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// copyFixture copies the fixture in dir, without its expect.json, into a
// new directory, and returns that directory.
func copyFixture(t *testing.T, dir string) string {
	t.Helper()
	dest := t.TempDir()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == filepath.Join(dir, "expect.json") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dest, rel)), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dest
}

// config returns the run's configuration for the copy of a fixture in dir:
// the confirmation of case.json, and the selection and the suite of
// expect.json.
func config(t *testing.T, dir string, c caseFile, e expectFile) run.Config {
	t.Helper()
	cfg := run.Config{Dir: dir, Env: os.Environ(), Confirm: c.Confirm}
	for _, entry := range e.Lines {
		file, lines, _ := strings.Cut(entry, ":")
		first, last, _ := strings.Cut(lines, "-")
		f, err1 := strconv.Atoi(first)
		l, err2 := strconv.Atoi(last)
		if err1 != nil || err2 != nil {
			t.Fatalf("expect.json has the selection %q", entry)
		}
		cfg.Lines = append(cfg.Lines, run.Lines{Path: filepath.Join(dir, file), First: f, Last: l})
	}
	for _, s := range e.Suite {
		cfg.Suite = append(cfg.Suite, "./"+s)
	}
	return cfg
}

// id names a mutant within its case.
func id(scope, kind string, nth int) string { return fmt.Sprintf("%s %s %d", scope, kind, nth) }

func TestCorpus(t *testing.T) {
	t.Parallel()
	cases, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the vendored corpus has no case")
	}
	for _, entry := range cases {
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var c caseFile
			var e expectFile
			decode(t, filepath.Join(corpus, name, "case.json"), &c)
			decode(t, filepath.Join(corpus, name, "go", "expect.json"), &e)
			dir := copyFixture(t, filepath.Join(corpus, name, "go"))
			rec, err := run.Run(context.Background(), config(t, dir, c, e))
			if err != nil {
				t.Fatal(err)
			}
			compare(t, c, e, rec)
		})
	}
}

// compare fails t for every difference between the record of a case's Go
// fixture and what case.json and expect.json state.
func compare(t *testing.T, c caseFile, e expectFile, rec *record.Record) {
	t.Helper()
	verdicts := map[string]string{}
	confirmed := map[string]bool{}
	for _, m := range c.Mutants {
		if len(m.Languages) == 0 || slices.Contains(m.Languages, "go") {
			verdicts[id(m.Scope, m.Kind, m.Nth)] = m.Verdict
			confirmed[id(m.Scope, m.Kind, m.Nth)] = m.Confirmed
		}
	}
	expected := map[string]expectMutant{}
	for _, m := range e.Mutants {
		expected[id(m.Scope, m.Kind, m.Nth)] = m
	}
	seen := map[string]int{}
	for _, got := range rec.Mutants {
		scopeKind := got.Scope + " " + got.Kind
		key := id(got.Scope, got.Kind, seen[scopeKind])
		seen[scopeKind]++
		want, ok := expected[key]
		if !ok {
			t.Errorf("the record has %s, which expect.json does not state", key)
			continue
		}
		delete(expected, key)
		if got.Key != want.Key || got.File != want.File || got.Start != want.Start || got.End != want.End ||
			got.Original != want.Original || got.Replacement != want.Replacement || got.Rule != want.Rule ||
			want.Reason != "" && got.Reason != want.Reason {
			t.Errorf("%s:\n got  %s %s %v-%v %q -> %q rule %q reason %q\n want %s %s %v-%v %q -> %q rule %q reason %q",
				key, got.Key, got.File, got.Start, got.End, got.Original, got.Replacement, got.Rule, got.Reason,
				want.Key, want.File, want.Start, want.End, want.Original, want.Replacement, want.Rule, want.Reason)
		}
		verdict := verdicts[key]
		if verdict == record.Exhausted && (len(rec.Limits) == 0 || rec.Limits[0].MemoryCeilingBytes == nil) {
			verdict = record.TimedOut
		}
		if got.Verdict != verdict {
			t.Errorf("%s has the verdict %s, want %s", key, got.Verdict, verdict)
		}
		if got.Confirmed != confirmed[key] {
			t.Errorf("%s is confirmed %v, want %v", key, got.Confirmed, confirmed[key])
		}
		if len(want.Tests) > 0 && !sameSet(got.Tests, want.Tests) {
			t.Errorf("%s names the tests %q, want %q", key, got.Tests, want.Tests)
		}
		if len(want.CoveredBy) > 0 && !sameSet(got.CoveredBy, want.CoveredBy) {
			t.Errorf("%s names the covering tests %q, want %q", key, got.CoveredBy, want.CoveredBy)
		}
	}
	for key := range expected {
		t.Errorf("the record lacks %s", key)
	}
	if len(rec.Skipped) != len(e.Skipped) {
		t.Errorf("skipped %v, want %v", rec.Skipped, e.Skipped)
	} else {
		for i := range rec.Skipped {
			if rec.Skipped[i] != e.Skipped[i] {
				t.Errorf("skipped %v, want %v", rec.Skipped[i], e.Skipped[i])
			}
		}
	}
	if !slices.Equal(rec.Generated, e.Generated) {
		t.Errorf("generated %v, want %v", rec.Generated, e.Generated)
	}
	var codes []string
	for _, err := range rec.Errors {
		codes = append(codes, err.Code)
	}
	if !sameSet(codes, c.Errors) {
		t.Errorf("run errors %v, want the codes %q", rec.Errors, c.Errors)
	}
	var suite []string
	for _, s := range e.Suite {
		suite = append(suite, rec.Target.Name+"/"+s)
	}
	if !sameSet(rec.Suite, suite) {
		t.Errorf("suite %q, want %q", rec.Suite, suite)
	}
	if ordinary := rec.Control != nil && rec.Control.Ordinary != nil; ordinary != c.Confirm {
		t.Errorf("the record states an ordinary control run %v, and case.json confirms %v", ordinary, c.Confirm)
	}
}

// sameSet reports whether a and b hold the same strings, in any order.
func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}
