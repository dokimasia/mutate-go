// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// The names of the files of addSub.
const (
	subFile     = "sub.go"
	subTestFile = "sub_test.go"
)

// addSub is a module whose test of Add checks Add, in a file of its own, and
// whose test of Sub calls Sub without a check.
var addSub = map[string]string{
	addFile: add,
	addTestFile: `package fixture

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`,
	subFile:     "package fixture\n\nfunc Sub(a, b int) int { return a - b }\n",
	subTestFile: "package fixture\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {\n\tSub(1, 1)\n}\n",
}

// addSubVerdicts are the verdicts of addSub's mutants.
const addSubVerdicts = `Add sbr-zero 0: killed [TestAdd]
Add aor 0: killed [TestAdd]
Sub sbr-zero 0: survived
Sub aor 0: survived
`

// The mutants of addSub by scope, as observedAt writes them.
const (
	addMutants = "Add sbr-zero 0\nAdd aor 0\n"
	subMutants = "Sub sbr-zero 0\nSub aor 0\n"
)

// The starts of the earlier runs of the cases, before every run of a test.
const (
	earlierStart = "2026-01-02T00:00:00Z"
	olderStart   = "2026-01-01T00:00:00Z"
)

// digestPattern is the pattern of a digest of an observation.
const digestPattern = `^sha256:[0-9a-f]{64}$`

// earlierAt returns a copy of rec whose run started at the time at: its
// start, its observation and the observation of each of its verdicts.
func earlierAt(rec *record.Record, at string) *record.Record {
	e := *rec
	e.StartedAt = at
	e.Observations = slices.Clone(rec.Observations[:1])
	e.Observations[0].StartedAt = at
	e.Mutants = slices.Clone(rec.Mutants)
	for i := range e.Mutants {
		if e.Mutants[i].ObservedAt != "" {
			e.Mutants[i].ObservedAt = at
		}
	}
	return &e
}

// observedAt returns the scope, the kind and the position of each mutant of
// rec whose verdict states the observation of start, one per line, as
// verdicts writes them.
func observedAt(rec *record.Record, start string) string {
	var b strings.Builder
	seen := map[string]int{}
	for _, m := range rec.Mutants {
		id := m.Scope + " " + string(m.Kind)
		if m.ObservedAt == start {
			fmt.Fprintf(&b, "%s %d\n", id, seen[id])
		}
		seen[id]++
	}
	return b.String()
}

// observeScope moves the verdicts of the mutants of scope in e to a second
// observation that started at at, with the files of e's observation and
// the digests of changed in place of theirs.
func observeScope(e *record.Record, scope, at string, changed map[string]string) {
	o := e.Observations[0]
	o.StartedAt = at
	o.Files = maps.Clone(o.Files)
	maps.Copy(o.Files, changed)
	e.Observations = append(e.Observations, o)
	for i := range e.Mutants {
		if e.Mutants[i].Scope == scope {
			e.Mutants[i].ObservedAt = at
		}
	}
}

// digestOf returns the digest that an observation states of a file whose
// content is text.
func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return record.DigestPrefix + hex.EncodeToString(sum[:])
}

func TestHistory(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("states the run's own observation first", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, addSub), run.Config{})
			assert.Equal(t, verdicts(rec), addSubVerdicts, "each mutant has the verdict of its run")
			assert.Length(t, rec.Observations, 1, "the record states the run's observation alone")
			o := rec.Observations[0]
			expect.Equal(t, o.StartedAt, rec.StartedAt, "the observation is the run's")
			expect.Equal(t, o.Files[addFile], digestOf(add), "it states the digest of each file of the package")
			expect.Equal(t, o.Files[goModFile], digestOf(goMod), "and of its data files")
			expect.Matches(t, o.Requirements, digestPattern, "the requirements digest is a SHA-256 digest")
			expect.Equal(t, observedAt(rec, rec.StartedAt), addMutants+subMutants,
				"the run observed the verdict of each mutant that it ran")
			expect.Nil(t, rec.Sample, "a run without a mutant that is not-run states no sample")
		})

		t.Run("states the files of the packages of the module that the package imports", func(t *testing.T) {
			t.Parallel()
			dep := "package dep\n\nconst One = 1\n"
			dir := module(t, map[string]string{
				addFile:         "package fixture\n\nimport \"fixture/dep\"\n\nfunc Add(a int) int { return a + dep.One }\n",
				"dep/dep.go":    dep,
				"dep/notes.txt": "notes\n",
			})
			rec := runIn(t, dir, run.Config{})
			assert.NotEmpty(t, rec.Observations, "the record states the run's observation")
			expect.Equal(t, rec.Observations[0].Files["dep/dep.go"], digestOf(dep),
				"the observation states the code of the imported package")
			expect.Equal(t, rec.Observations[0].Files["dep/notes.txt"], digestOf("notes\n"),
				"and its data files")
		})

		t.Run("states the digest of what the snapshot states of a link", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a link needs a privilege on Windows")
			}
			dir := module(t, addSub)
			files.Write(t, dir, files.Tree{"link": files.Link(addFile)})
			rec := runIn(t, dir, run.Config{})
			assert.NotEmpty(t, rec.Observations, "the record states the run's observation")
			assert.Equal(t, rec.Observations[0].Files["link"], digestOf("link "+addFile),
				"the link's digest is the digest of its target's name")
		})

		t.Run("states the load error of a file of an imported package that does not read", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a file without read permission is readable on Windows")
			}
			dir := module(t, map[string]string{
				addFile:      "package fixture\n\nimport \"fixture/dep\"\n\nfunc Add(a int) int { return a + dep.One }\n",
				"dep/dep.go": "package dep\n\nconst One = 1\n",
			})
			files.Write(t, dir, files.Tree{"dep/locked.txt": files.Text("locked\n").WithMode(0o000)})
			rec := runIn(t, dir, run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorLoad}, "the imported package does not read")
			assert.Contains(t, rec.Errors[0].Message, "locked.txt", "the message names the file")
		})

		t.Run("states the same requirements digest until a requirement file changes", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			first := runIn(t, dir, run.Config{List: true})
			second := runIn(t, dir, run.Config{List: true})
			files.Write(t, dir, files.Tree{"go.sum": files.Text("")})
			third := runIn(t, dir, run.Config{List: true})
			assert.Equal(t, second.Observations[0].Requirements, first.Observations[0].Requirements,
				"two runs of the same requirements have one digest")
			assert.NotEqual(t, third.Observations[0].Requirements, first.Observations[0].Requirements,
				"a new go.sum changes the digest")
		})

		t.Run("keeps the reused verdicts of the mutants that the limit leaves unrun", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 1})
			assert.Equal(t, verdicts(rec), addSubVerdicts, "each mutant has its verdict")
			assert.Equal(t, strings.Count(observedAt(rec, rec.StartedAt), "\n"), 1, "one mutant ran")
			assert.Equal(t, strings.Count(observedAt(rec, earlierStart), "\n"), 3,
				"the three others keep their earlier verdicts")
			expect.False(t, rec.Failed(), "the reused verdicts do not fail the run")
			expect.Nil(t, rec.Sample, "every mutant has a verdict, so the record states no sample")
			expect.Equal(t, rec.Score, new(0.5), "the score counts the reused verdicts")
			expect.Equal(t, rec.Observations, []record.Observation{rec.Observations[0], earlier.Observations[0]},
				"the record states the run's observation and then the earlier one that the verdicts name")
		})

		t.Run("states the reused mutant's earlier tests and time", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
			earlier.Mutants[0].Seconds = new(42.0)
			earlier.Mutants[0].CoveredBy = []string{"TestCovering"}
			files.Write(t, dir, files.Tree{subFile: files.Text(addSub[subFile] + "\n// Sub changed.\n")})
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 2})
			assert.Equal(t, observedAt(rec, earlierStart), addMutants, "the kills of Add are reused")
			expect.Equal(t, rec.Mutants[0].Seconds, new(42.0), "a reused verdict states the earlier time")
			expect.Equal(t, rec.Mutants[0].CoveredBy, []string{"TestCovering"}, "and the earlier covering tests")
			expect.Equal(t, rec.Mutants[0].Tests, []string{"TestAdd"}, "and the earlier killing test")
		})

		changes := []struct {
			name string
			// give lists the files that change after the earlier run.
			give map[string]string
			// earlier changes the earlier record, when it is not nil.
			earlier func(e *record.Record)
			// confirm runs the second run under Confirm.
			confirm bool
			// want lists the mutants that run again.
			want string
		}{
			{
				name: "runs a kill again whose file changed",
				give: map[string]string{addFile: add + "\n// Add changed.\n"},
				want: addMutants,
			},
			{
				name: "runs a kill again whose test's file changed",
				give: map[string]string{addTestFile: addSub[addTestFile] + "\n// TestAdd changed.\n"},
				want: addMutants + subMutants,
			},
			{
				name: "runs a survivor again whose file changed",
				give: map[string]string{subFile: addSub[subFile] + "\n// Sub changed.\n"},
				want: subMutants,
			},
			{
				name: "runs a survivor again after a test file of the package changed",
				give: map[string]string{subTestFile: addSub[subTestFile] + "\n// TestSub changed.\n"},
				want: subMutants,
			},
			{
				name: "runs a survivor again after a data file was added",
				give: map[string]string{"testdata/input.txt": "input\n"},
				want: subMutants,
			},
			{
				name: "runs a kill again whose test the package no longer declares",
				earlier: func(e *record.Record) {
					e.Mutants[1].Tests = []string{"TestGone"}
				},
				want: "Add aor 0\n",
			},
			{
				name: "runs a mutant again whose earlier verdict is no-coverage",
				earlier: func(e *record.Record) {
					e.Mutants[1].Verdict = spec.NoCoverage
				},
				want: "Add aor 0\n",
			},
			{
				name: "runs a mutant again whose observation the earlier record lacks",
				earlier: func(e *record.Record) {
					e.Mutants[1].ObservedAt = olderStart
				},
				want: "Add aor 0\n",
			},
			{
				name: "runs a mutant again whose key the earlier record lacks",
				earlier: func(e *record.Record) {
					e.Mutants[1].Key = missing
				},
				want: "Add aor 0\n",
			},
			{
				name: "runs a mutant again whose earlier run had other requirements",
				earlier: func(e *record.Record) {
					e.Observations[0].Requirements = digestOf(missing)
				},
				want: addMutants + subMutants,
			},
			{
				name:    "runs every mutant again under other settings",
				confirm: true,
				want:    addMutants + subMutants,
			},
		}
		for _, tt := range changes {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				dir := module(t, addSub)
				earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
				if tt.earlier != nil {
					tt.earlier(earlier)
				}
				tree := files.Tree{}
				for name, text := range tt.give {
					tree[name] = files.Text(text)
				}
				files.Write(t, dir, tree)
				runs := strings.Count(tt.want, "\n")
				rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: runs, Confirm: tt.confirm})
				expect.Equal(t, observedAt(rec, rec.StartedAt), tt.want, "the run tests the mutants of changed inputs")
				expect.Empty(t, rec.Errors, "the run states no run error")
				expect.Nil(t, rec.Sample, "every mutant has a verdict")
			})
		}

		t.Run("states the limit of a run that leaves mutants without a reusable verdict unrun", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
			files.Write(t, dir, files.Tree{addTestFile: files.Text(addSub[addTestFile] + "\n// TestAdd changed.\n")})
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 2})
			assert.Equal(t, strings.Count(verdicts(rec), string(spec.NotRun)), 2, "two mutants are not-run")
			assert.NotNil(t, rec.Sample, "the record states the sample of the mutants with a verdict")
			expect.Nil(t, rec.Sample.Before, "the sample of a run with a history states no first key")
			expect.Equal(t, rec.Sample.Limit, new(2), "the sample states the limit")
			expect.False(t, rec.Failed(), "the limit does not fail the run")
		})

		t.Run("runs the mutants whose wider inputs changed before the others", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), olderStart)
			// Add's verdicts come from a newer run, after which sub.go changed,
			// so their wider inputs changed. Sub's older verdicts saw the files
			// of today.
			observeScope(earlier, "Add", earlierStart, map[string]string{subFile: digestOf(missing)})
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 2})
			assert.Equal(t, observedAt(rec, rec.StartedAt), addMutants, "the verdicts of Add run first")
		})

		// Each case gives the mutants of one scope the older observation, so
		// one of the two cases runs them against the order of their keys.
		ages := []struct {
			name  string
			give  string
			other string
		}{
			{"runs the verdicts of Add first when they are the oldest", "Add", "Sub"},
			{"runs the verdicts of Sub first when they are the oldest", "Sub", "Add"},
		}
		for _, tt := range ages {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				dir := module(t, addSub)
				earlier := earlierAt(runIn(t, dir, run.Config{History: true}), olderStart)
				observeScope(earlier, tt.other, earlierStart, nil)
				rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 2})
				assert.Equal(t, observedAt(rec, rec.StartedAt), observedAt(earlier, olderStart),
					"the mutants of "+tt.give+" run first")
			})
		}

		t.Run("reuses a survivor after a data file of an imported package at the root changed", func(t *testing.T) {
			t.Parallel()
			root := module(t, map[string]string{
				"root.go":   "package fixture\n\nconst One = 1\n",
				"notes.txt": "notes\n",
				"sub/sub.go": "package sub\n\nimport \"fixture\"\n\n" +
					"func Sub(a int) int { return a - fixture.One }\n",
				"sub/sub_test.go": "package sub\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {\n\tSub(1)\n}\n",
			})
			dir := filepath.Join(root, "sub")
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
			files.Write(t, root, files.Tree{"notes.txt": files.Text("changed notes\n")})
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier, Sample: 1})
			assert.Equal(t, verdicts(rec), "Sub sbr-zero 0: survived\nSub aor 0: survived\n",
				"each mutant has its verdict")
			expect.Equal(t, strings.Count(observedAt(rec, earlierStart), "\n"), 1,
				"the file of the imported package is no data file of the suite")
			expect.Nil(t, rec.Sample, "every mutant has a verdict")
		})

		t.Run("reuses the kill of a test of another package of the suite", func(t *testing.T) {
			t.Parallel()
			dir := module(t, scale)
			cfg := run.Config{History: true, Suite: []string{conformancePattern}}
			earlier := earlierAt(runIn(t, dir, cfg), earlierStart)
			// The own test's kill is older, so it runs first while the kill of
			// the suite's test is reusable.
			earlier.Observations = append(earlier.Observations, earlier.Observations[0])
			earlier.Observations[1].StartedAt = olderStart
			earlier.Mutants[0].ObservedAt = olderStart
			cfg.Earlier, cfg.Sample = earlier, 1
			rec := runIn(t, dir, cfg)
			assert.Equal(t, observedAt(rec, rec.StartedAt), "Scale sbr-zero 0\n", "the own test's kill runs")
			assert.Equal(t, verdicts(rec), `Scale sbr-zero 0: killed [TestScaleByOne]
Scale aor 0: killed [fixture/conformance: TestScale]
`, "the kill of the suite's test is reused")
			assert.Equal(t, rec.Mutants[1].ObservedAt, earlierStart, "the kill keeps its observation")
		})

		t.Run("states the load error of a test file that does not parse", func(t *testing.T) {
			t.Parallel()
			dir := module(t, addSub)
			earlier := earlierAt(runIn(t, dir, run.Config{History: true}), earlierStart)
			files.Write(t, dir, files.Tree{"broken_test.go": files.Text("package fixture\n\nfunc {\n")})
			rec := runIn(t, dir, run.Config{History: true, Earlier: earlier})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorLoad}, "the test file does not parse")
			assert.Contains(t, rec.Errors[0].Message, "broken_test.go", "the message names the file")
			assert.Equal(t, rec.Mutants[0].Reason, "the test files of the suite do not parse",
				"the record states why no mutant runs")
		})
	})
}
