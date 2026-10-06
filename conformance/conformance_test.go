// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/selection"
	"go.dokimi.dev/mutate/internal/spec"
)

// corpus is the directory of the vendored corpus.
var corpus = filepath.Join("spec", "corpus")

// The files of a corpus case: case.json in the case's directory, and
// expect.json in the directory of each language's fixture.
const (
	caseFileName   = "case.json"
	expectFileName = "expect.json"
)

// localPattern starts a go list pattern that names a directory relative to
// the package directory.
const localPattern = "./"

// caseFile is a case's case.json: the mutants and the run errors of the
// case in every language, and the options of its run.
type caseFile struct {
	Case             string           `json:"case"`
	Confirm          bool             `json:"confirm"`
	IncludeGenerated bool             `json:"includeGenerated"`
	Sample           int              `json:"sample"`
	Proves           string           `json:"proves"`
	Fixture          string           `json:"fixture"`
	Mutants          []caseMutant     `json:"mutants"`
	Errors           []spec.ErrorCode `json:"errors"`
}

// caseMutant is a mutant of case.json, which nth identifies among the
// mutants of its scope and kind. Languages restricts the mutant to the
// fixtures of the languages that it lists, and is empty for a mutant of
// every fixture.
type caseMutant struct {
	Scope     string       `json:"scope"`
	Kind      spec.Kind    `json:"kind"`
	Nth       int          `json:"nth"`
	Verdict   spec.Verdict `json:"verdict"`
	Confirmed bool         `json:"confirmed"`
	Languages []string     `json:"languages"`
}

// expectFile is a fixture's expect.json: what the record states beyond the
// verdicts.
type expectFile struct {
	Language  string             `json:"language"`
	Lines     []string           `json:"lines"`
	Suite     []string           `json:"suite"`
	Mutants   []expectMutant     `json:"mutants"`
	Skipped   []record.Skip      `json:"skipped"`
	Generated []record.Generated `json:"generated"`
	Errors    []spec.ErrorCode   `json:"errors"`
}

// expectMutant is a mutant of expect.json. Reason, Tests and CoveredBy are
// empty where expect.json does not state them.
type expectMutant struct {
	Scope       string          `json:"scope"`
	Kind        spec.Kind       `json:"kind"`
	Nth         int             `json:"nth"`
	Occurrence  int             `json:"occurrence"`
	Key         string          `json:"key"`
	File        string          `json:"file"`
	Start       record.Position `json:"start"`
	End         record.Position `json:"end"`
	Original    string          `json:"original"`
	Replacement string          `json:"replacement"`
	Rule        spec.Family     `json:"rule"`
	Reason      string          `json:"reason"`
	NotViable   bool            `json:"notViable"`
	Tests       []string        `json:"tests"`
	CoveredBy   []string        `json:"coveredBy"`
}

// mutantID identifies a mutant within its case: its scope, its kind, and
// its position from 0 in source order among the mutants of its scope and
// kind.
type mutantID struct {
	Scope string
	Kind  spec.Kind
	Nth   int
}

// String returns the scope, the kind and the position, separated by
// spaces, as a failure names the mutant.
func (m mutantID) String() string { return fmt.Sprintf("%s %s %d", m.Scope, m.Kind, m.Nth) }

// stated is what the corpus states of one mutant of a record: its key, its
// position, its source and its replacement, its rule and its reason, its
// verdict, and whether a confirmation run decided the verdict.
type stated struct {
	Key, File             string
	Start, End            record.Position
	Original, Replacement string
	Rule                  spec.Family
	Reason                string
	Verdict               spec.Verdict
	Confirmed             bool
}

func TestCorpus(t *testing.T) {
	t.Parallel()
	cases, err := os.ReadDir(corpus)
	assert.NoError(t, err, "the vendored corpus reads")
	assert.NotEmpty(t, cases, "the vendored corpus has a case")
	language := spec.Load().Overlay.Language
	for _, entry := range cases {
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var c caseFile
			var e expectFile
			decode(t, filepath.Join(corpus, name, caseFileName), &c)
			decode(t, filepath.Join(corpus, name, language, expectFileName), &e)
			assert.Equal(t, e.Language, language, "expect.json states the fixture of the engine's language")
			dir := copyFixture(t, filepath.Join(corpus, name, language))
			rec, err := run.Run(context.Background(), config(t, dir, c, e))
			assert.NoError(t, err, "the engine returns the record of the fixture's run")
			compare(t, language, c, e, rec)
		})
	}
}

// decode reads the JSON file at path into v, and stops the test when the
// file does not read or names a field that v's type lacks.
func decode(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.NoError(t, err, path+" reads")
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	assert.NoError(t, dec.Decode(v), path+" decodes with no field that the test does not know")
}

// copyFixture copies the fixture in dir, without its expect.json, into a
// new directory, and returns that directory.
func copyFixture(t *testing.T, dir string) string {
	t.Helper()
	dest := t.TempDir()
	assert.NoError(t, os.CopyFS(dest, os.DirFS(dir)), "the fixture copies")
	assert.NoError(t, os.Remove(filepath.Join(dest, expectFileName)), "the copy leaves out expect.json")
	return dest
}

// config returns the run's configuration for the copy of a fixture in dir:
// the options of case.json, and the selection and the suite of expect.json.
func config(t *testing.T, dir string, c caseFile, e expectFile) run.Config {
	t.Helper()
	cfg := run.Config{
		Dir:              dir,
		Env:              os.Environ(),
		Confirm:          c.Confirm,
		IncludeGenerated: c.IncludeGenerated,
		Sample:           c.Sample,
	}
	for _, entry := range e.Lines {
		lines, err := selection.ParseEntry(filepath.Join(dir, entry))
		assert.NoError(t, err, "the selection of expect.json parses")
		cfg.Lines = append(cfg.Lines, lines)
	}
	for _, s := range e.Suite {
		cfg.Suite = append(cfg.Suite, localPattern+s)
	}
	return cfg
}

// compare records a failure for every difference between the record of a
// case's fixture in language and what case.json and expect.json state, as
// the corpus defines a pass. The test's log states the record's run errors,
// which go test prints when the test fails.
func compare(t *testing.T, language string, c caseFile, e expectFile, rec *record.Record) {
	t.Helper()
	for _, err := range rec.Errors {
		t.Logf("the record states the run error %s: %s", err.Code, err.Message)
	}
	cases := map[mutantID]caseMutant{}
	for _, m := range c.Mutants {
		if len(m.Languages) == 0 || slices.Contains(m.Languages, language) {
			cases[mutantID{Scope: m.Scope, Kind: m.Kind, Nth: m.Nth}] = m
		}
	}
	expected := map[mutantID]expectMutant{}
	want := make([]mutantID, 0, len(e.Mutants))
	for _, m := range e.Mutants {
		key := mutantID{Scope: m.Scope, Kind: m.Kind, Nth: m.Nth}
		expected[key] = m
		want = append(want, key)
	}
	// count maps the scope and the kind of each mutant, an ID without its
	// position, to the number of the record's mutants of both so far.
	count := map[mutantID]int{}
	got := make([]mutantID, 0, len(rec.Mutants))
	for _, m := range rec.Mutants {
		group := mutantID{Scope: m.Scope, Kind: m.Kind}
		key := mutantID{Scope: m.Scope, Kind: m.Kind, Nth: count[group]}
		count[group]++
		got = append(got, key)
		if x, ok := expected[key]; ok {
			compareMutant(t, key, m, x, cases[key], rec)
		}
	}
	expect.Permutation(t, got, want, "the record lists the mutants of expect.json and no other")
	expect.Equal(t, rec.Skipped, e.Skipped, "the record lists the skipped sites of expect.json", expect.EquateEmpty())
	expect.Equal(t, rec.Generated, e.Generated, "the record lists the generated files of expect.json",
		expect.EquateEmpty())
	codes := make([]spec.ErrorCode, 0, len(rec.Errors))
	for _, err := range rec.Errors {
		codes = append(codes, err.Code)
	}
	expect.Permutation(t, codes, c.Errors, "the record states the run errors of case.json", expect.EquateEmpty())
	suite := make([]string, 0, len(e.Suite))
	for _, s := range e.Suite {
		suite = append(suite, path.Join(rec.Target.Name, s))
	}
	expect.Permutation(t, rec.Suite, suite, "the record names the packages of the suite of expect.json",
		expect.EquateEmpty())
	expect.Equal(t, rec.Control != nil && rec.Control.Ordinary != nil, c.Confirm,
		"the record states an ordinary control run exactly when case.json confirms")
	if c.Sample > 0 {
		assert.NotNil(t, rec.Sample, "the record of a run that case.json samples states a sample")
		expect.Equal(t, rec.Sample.Limit, &c.Sample, "the sample states the limit of case.json")
		expect.Equal(t, rec.Score, rec.Sample.Score, "the run's score is the score of its sample")
	}
}

// compareMutant records a failure for every difference between the
// record's mutant m, which key identifies, and what expect.json states in x
// and case.json in c. A record that states no memory ceiling gives timed-out
// in place of exhausted, and a reason counts only where expect.json states
// one.
func compareMutant(t *testing.T, key mutantID, m record.Mutant, x expectMutant, c caseMutant, rec *record.Record) {
	t.Helper()
	verdict := c.Verdict
	if verdict == spec.Exhausted && (len(rec.Limits) == 0 || rec.Limits[0].MemoryCeilingBytes == nil) {
		verdict = spec.TimedOut
	}
	reason := x.Reason
	if reason == "" {
		reason = m.Reason
	}
	expect.Equal(t,
		stated{
			Key: m.Key, File: m.File, Start: m.Start, End: m.End, Original: m.Original, Replacement: m.Replacement,
			Rule: m.Rule, Reason: m.Reason, Verdict: m.Verdict, Confirmed: m.Confirmed,
		},
		stated{
			Key: x.Key, File: x.File, Start: x.Start, End: x.End, Original: x.Original, Replacement: x.Replacement,
			Rule: x.Rule, Reason: reason, Verdict: verdict, Confirmed: c.Confirmed,
		},
		key.String()+" has the fields of expect.json and the verdict of case.json")
	if len(x.Tests) > 0 {
		expect.Permutation(t, m.Tests, x.Tests, key.String()+" names the tests of expect.json")
	}
	if len(x.CoveredBy) > 0 {
		expect.Permutation(t, m.CoveredBy, x.CoveredBy, key.String()+" names the covering tests of expect.json")
	}
}
