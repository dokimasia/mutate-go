// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// validate checks the JSON value v against the schema node of the record
// schema, whose definitions are defs, and returns every violation. It knows
// the keywords that the record schema uses, and reports any other keyword
// as a violation, so a schema that grows a keyword fails the test until the
// validator knows it.
func validate(defs map[string]any, schema map[string]any, v any, path string) []string {
	var out []string
	fail := func(format string, args ...any) { out = append(out, path+": "+fmt.Sprintf(format, args...)) }
	for key, rule := range schema {
		switch key {
		case "$schema", "$id", "title", "description", "$defs":
		case "$ref":
			name := strings.TrimPrefix(rule.(string), "#/$defs/")
			out = append(out, validate(defs, defs[name].(map[string]any), v, path)...)
		case "type":
			types := []any{rule}
			if list, ok := rule.([]any); ok {
				types = list
			}
			matched := false
			for _, t := range types {
				matched = matched || isType(v, t.(string))
			}
			if !matched {
				fail("%v is not of type %v", v, rule)
			}
		case "const":
			if !reflect.DeepEqual(v, rule) {
				fail("%v is not %v", v, rule)
			}
		case "enum":
			found := false
			for _, e := range rule.([]any) {
				found = found || e == v
			}
			if !found {
				fail("%v is not in %v", v, rule)
			}
		case "pattern":
			if s, ok := v.(string); ok && !regexp.MustCompile(rule.(string)).MatchString(s) {
				fail("%q does not match %s", s, rule)
			}
		case "minLength":
			if s, ok := v.(string); ok && utf8.RuneCountInString(s) < int(rule.(float64)) {
				fail("%q is shorter than %v", s, rule)
			}
		case "maxLength":
			if s, ok := v.(string); ok && utf8.RuneCountInString(s) > int(rule.(float64)) {
				fail("%q is longer than %v", s, rule)
			}
		case "minimum":
			if n, ok := v.(float64); ok && n < rule.(float64) {
				fail("%v is below %v", n, rule)
			}
		case "maximum":
			if n, ok := v.(float64); ok && n > rule.(float64) {
				fail("%v is above %v", n, rule)
			}
		case "exclusiveMinimum":
			if n, ok := v.(float64); ok && n <= rule.(float64) {
				fail("%v is not above %v", n, rule)
			}
		case "format":
			if s, ok := v.(string); ok && rule == "date-time" {
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					fail("%q is not a date-time", s)
				}
			}
		case "required":
			obj, _ := v.(map[string]any)
			for _, name := range rule.([]any) {
				if _, ok := obj[name.(string)]; obj != nil && !ok {
					fail("lacks %s", name)
				}
			}
		case "properties":
			obj, _ := v.(map[string]any)
			for name, sub := range rule.(map[string]any) {
				if value, ok := obj[name]; ok {
					out = append(out, validate(defs, sub.(map[string]any), value, path+"."+name)...)
				}
			}
		case "additionalProperties":
			obj, _ := v.(map[string]any)
			props, _ := schema["properties"].(map[string]any)
			for name := range obj {
				if _, ok := props[name]; !ok && rule == false {
					fail("has the property %s, which the schema does not declare", name)
				}
			}
		case "items":
			list, _ := v.([]any)
			for i, item := range list {
				out = append(out, validate(defs, rule.(map[string]any), item, fmt.Sprintf("%s[%d]", path, i))...)
			}
		case "oneOf":
			passed := 0
			for _, sub := range rule.([]any) {
				if len(validate(defs, sub.(map[string]any), v, path)) == 0 {
					passed++
				}
			}
			if passed != 1 {
				fail("%v matches %d of the oneOf schemas, want 1", v, passed)
			}
		default:
			fail("the schema uses the keyword %s, which the validator does not know", key)
		}
	}
	return out
}

func isType(v any, t string) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		n, ok := v.(float64)
		return ok && n == float64(int64(n))
	case "null":
		return v == nil
	}
	return false
}

// schema returns the vendored record schema, decoded.
func schema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "conformance", "spec", "record.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// conforms fails t when the record file at path breaks the vendored
// record schema.
func conforms(t *testing.T, path string) {
	t.Helper()
	schema := schema(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if problems := validate(schema["$defs"].(map[string]any), schema, v, "record"); len(problems) != 0 {
		sort.Strings(problems)
		t.Errorf("the record breaks the schema:\n%s", strings.Join(problems, "\n"))
	}
}

// full returns a record whose every field is set.
func full() *record.Record {
	ceiling, peak, seconds := int64(805306368), int64(67108864), 0.01
	return &record.Record{
		Record:     "dokimi-mutate",
		Version:    1,
		Catalogue:  "1.0.0",
		Overlay:    "1.0.0",
		Engine:     record.Engine{Name: "mutate-go", Version: "(devel)"},
		Toolchain:  "go1.27.1",
		Target:     record.Target{Language: "go", Name: "github.com/google/btree"},
		Root:       "/work/btree",
		StartedAt:  "2026-10-02T21:12:51Z",
		FinishedAt: "2026-10-02T21:14:07Z",
		Inputs:     "sha256:" + strings.Repeat("9c", 32),
		Suite:      []string{"github.com/google/btree/conformance"},
		Selection:  []record.Range{{File: "btree.go", First: 3, Last: 9}},
		Limits: []record.Limits{
			{Target: "github.com/google/btree", DeadlineSeconds: 4, MemoryCeilingBytes: &ceiling},
			{Target: "github.com/google/btree/conformance", DeadlineSeconds: 6, MemoryCeilingBytes: &ceiling},
		},
		Control: &record.Control{
			Opening:  record.Opening{Seconds: 0.2, PeakBytes: &peak, Sites: 410, SitesExecuted: 382},
			Ordinary: &record.Ordinary{Seconds: 0.1},
			Closing:  &record.Closing{Seconds: 0.2},
		},
		Errors: []record.RunError{},
		Mutants: []record.Mutant{{
			Key: "3f9a0c71d2b4e816", Kind: "lcr-false", File: "btree.go", Scope: "items.find",
			Start: record.Position{Line: 218, Column: 5}, End: record.Position{Line: 218, Column: 33},
			Original: "i > 0 && !less(s[i-1], item)", Replacement: "false", Verdict: record.Killed,
			Tests: []string{"TestBTreeG"}, CoveredBy: []string{"TestBTreeG", "TestDescendRangeG"},
			Seconds: &seconds, Confirmed: true,
		}, {
			Key: "0123456789abcdef", Kind: "sbr-delete", File: "btree.go", Scope: "items.find",
			Start: record.Position{Line: 3, Column: 2}, End: record.Position{Line: 3, Column: 9},
			Original: "f(x)", Replacement: "", Verdict: record.Suppressed, Rule: "logging",
		}},
		Skipped: []record.Skip{
			{
				File:   "btree.go",
				Start:  record.Position{Line: 136, Column: 36},
				End:    record.Position{Line: 136, Column: 41},
				Reason: "operand of type-parameter type",
			},
		},
		Generated: []record.Generated{{File: "btree_gen.go", Mutants: 12}},
	}
}

func TestRecord(t *testing.T) {
	t.Parallel()
	t.Run("the verdicts", func(t *testing.T) {
		t.Parallel()
		t.Run("are the verdicts of the protocol in its order", func(t *testing.T) {
			t.Parallel()
			var protocol []string
			for _, v := range spec.Load().Protocol.Verdicts {
				protocol = append(protocol, v.ID)
			}
			ours := []string{
				record.Killed, record.TimedOut, record.Exhausted, record.Survived, record.NoCoverage,
				record.NotViable, record.Suppressed, record.NotSelected, record.NotRun, record.Error,
			}
			if strings.Join(ours, " ") != strings.Join(protocol, " ") {
				t.Errorf("verdicts %v, and the protocol defines %v", ours, protocol)
			}
		})
	})
	t.Run("Failed", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			give func(r *record.Record)
			want bool
		}{
			{"returns false for a run with verdicts of every mutant", func(r *record.Record) {}, false},
			{
				"returns true for a run error",
				func(r *record.Record) { r.Errors = []record.RunError{{Code: "build", Message: "x"}} },
				true,
			},
			{
				"returns true for a mutant that did not run",
				func(r *record.Record) { r.Mutants[1].Verdict = record.NotRun },
				true,
			},
			{
				"returns true for a mutant whose run ended in an error",
				func(r *record.Record) { r.Mutants[1].Verdict = record.Error },
				true,
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				r := full()
				tt.give(r)
				if got := r.Failed(); got != tt.want {
					t.Errorf("Failed() = %v, want %v", got, tt.want)
				}
			})
		}
	})
	t.Run("SetScore", func(t *testing.T) {
		t.Parallel()
		verdicts := func(vs ...string) *record.Record {
			r := full()
			r.Mutants = nil
			for _, v := range vs {
				r.Mutants = append(r.Mutants, record.Mutant{Verdict: v})
			}
			return r
		}
		protocol := spec.Load().Protocol
		t.Run("divides the detected mutants by the detected and the undetected ones", func(t *testing.T) {
			t.Parallel()
			r := verdicts(record.Killed, record.TimedOut, record.Exhausted, record.Survived, record.NoCoverage,
				record.NotViable, record.Suppressed, record.NotSelected, record.Killed, record.Survived)
			r.SetScore(protocol)
			if r.Score == nil || *r.Score != 4.0/7.0 {
				t.Errorf("Score = %v, want 4/7", r.Score)
			}
		})
		t.Run("sets no score for a run that fails", func(t *testing.T) {
			t.Parallel()
			r := verdicts(record.Killed, record.NotRun)
			r.SetScore(protocol)
			if r.Score != nil {
				t.Errorf("Score = %v, want nil", *r.Score)
			}
		})
		t.Run("sets no score for a run without a detected or an undetected mutant", func(t *testing.T) {
			t.Parallel()
			r := verdicts(record.Suppressed, record.NotSelected)
			score := 1.0
			r.Score = &score
			r.SetScore(protocol)
			if r.Score != nil {
				t.Errorf("Score = %v, want nil", *r.Score)
			}
		})
		// keyed returns a record of mutants with the keys and the verdicts
		// of pairs, in that order.
		keyed := func(pairs ...string) *record.Record {
			r := full()
			r.Mutants = nil
			for i := 0; i < len(pairs); i += 2 {
				r.Mutants = append(r.Mutants, record.Mutant{Key: pairs[i], Verdict: pairs[i+1]})
			}
			return r
		}
		t.Run("samples the mutants whose keys sort before the least key of a not-run mutant", func(t *testing.T) {
			t.Parallel()
			r := keyed(
				"5", record.NotRun, "1", record.Killed, "4", record.Killed, "3", record.NotRun,
				"2", record.Survived, "0", record.Suppressed,
			)
			r.SetScore(protocol)
			if s := r.Sample; s == nil || s.Before != "3" || s.Detected != 1 || s.Undetected != 1 || s.Score == nil ||
				*s.Score != 0.5 {
				t.Errorf("Sample = %+v, want the keys before 3: 1 detected, 1 undetected, the score 0.5", s)
			}
		})
		t.Run("sets a sample without a score when no sampled mutant counts", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", record.NotRun, "0", record.Suppressed, "2", record.Killed)
			r.SetScore(protocol)
			if s := r.Sample; s == nil || s.Before != "1" || s.Detected != 0 || s.Undetected != 0 || s.Score != nil {
				t.Errorf("Sample = %+v, want the keys before 1 and no score", s)
			}
		})
		t.Run("sets no sample for a run with a run error", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", record.Killed, "2", record.NotRun)
			r.Errors = []record.RunError{{Code: "build", Message: "x"}}
			r.SetScore(protocol)
			if r.Sample != nil {
				t.Errorf("Sample = %+v, want nil", r.Sample)
			}
		})
		t.Run("sets no sample for a run in which every mutant has a verdict", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", record.Killed, "2", record.Error)
			r.Sample = &record.Sample{Before: "2"}
			r.SetScore(protocol)
			if r.Sample != nil {
				t.Errorf("Sample = %+v, want nil", r.Sample)
			}
		})
	})
	t.Run("Line", func(t *testing.T) {
		t.Parallel()
		m := record.Mutant{
			Key:         "3f9a0c71d2b4e816",
			Kind:        "ror-boundary",
			Start:       record.Position{Line: 7, Column: 5},
			Original:    "a < b",
			Replacement: "a <= b",
		}
		tests := []struct {
			name, verdict, replacement, reason, want string
		}{
			{
				"reports a survivor",
				record.Survived,
				"a <= b",
				"",
				"f.go:7:5: survived: a < b became a <= b (ror-boundary)",
			},
			{
				"reports a mutant without coverage as not covered",
				record.NoCoverage,
				"a <= b",
				"",
				"f.go:7:5: not covered: a < b became a <= b (ror-boundary)",
			},
			{
				"writes a verdict's hyphens as spaces",
				record.TimedOut,
				"a <= b",
				"",
				"f.go:7:5: timed out: a < b became a <= b (ror-boundary)",
			},
			{
				"reports an empty replacement as the removal of the original",
				record.Survived,
				"",
				"",
				"f.go:7:5: survived: a < b removed (ror-boundary)",
			},
			{
				"states the reason of an error",
				record.Error,
				"a <= b",
				"the run did not start",
				"f.go:7:5: error: a < b became a <= b (ror-boundary): the run did not start",
			},
			{
				"leaves out the reason of a suppressed mutant",
				record.Suppressed,
				"a <= b",
				"an equal value",
				"f.go:7:5: suppressed: a < b became a <= b (ror-boundary)",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				m := m
				m.Verdict, m.Replacement, m.Reason = tt.verdict, tt.replacement, tt.reason
				if got := m.Line("f.go"); got != tt.want {
					t.Errorf("Line() = %s\nwant      %s", got, tt.want)
				}
			})
		}
	})
	t.Run("Path", func(t *testing.T) {
		t.Parallel()
		t.Run("returns the file relative to the directory with its links resolved", func(t *testing.T) {
			t.Parallel()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "pkg"), 0o755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(filepath.Join(root, "pkg"), link); err != nil {
				t.Fatal(err)
			}
			r := &record.Record{Root: root}
			if got := r.Path("pkg/a.go", link); got != "a.go" {
				t.Errorf("Path() through a link = %s, want a.go", got)
			}
			if got := r.Path("b.go", filepath.Join(root, "pkg")); got != filepath.Join("..", "b.go") {
				t.Errorf("Path() = %s, want ../b.go", got)
			}
			if got := r.Path("b.go", filepath.Join(root, "missing")); got != filepath.Join("..", "b.go") {
				t.Errorf("Path() from a missing directory = %s, want ../b.go", got)
			}
		})
	})
	t.Run("Summary", func(t *testing.T) {
		t.Parallel()
		half := 0.5
		tests := []struct {
			name     string
			verdicts []string
			errors   []record.RunError
			sample   *record.Sample
			want     string
		}{
			{
				"states the detected mutants of those that the score counts, and each verdict's count",
				[]string{record.Killed, record.Survived},
				nil,
				nil,
				"1 of 2 mutants detected (50%): 1 killed, 1 survived",
			},
			{
				"rounds the percentage down",
				[]string{record.Killed, record.Killed, record.Survived},
				nil,
				nil,
				"2 of 3 mutants detected (66%): 2 killed, 1 survived",
			},
			{
				"states one mutant in the singular",
				[]string{record.Killed, record.Suppressed},
				nil,
				nil,
				"1 of 1 mutant detected (100%): 1 killed, 1 suppressed",
			},
			{
				"writes each verdict as a phrase, in the protocol's order",
				[]string{record.NotSelected, record.TimedOut, record.NoCoverage, record.NotViable, record.Killed},
				nil,
				nil,
				"2 of 3 mutants detected (66%): 1 killed, 1 timed out, 1 not covered, 1 not viable, 1 not selected",
			},
			{
				"states a run that fails in place of the detected mutants",
				[]string{record.Killed, record.NotRun},
				nil,
				nil,
				"the run failed: 1 killed, 1 not run",
			},
			{
				"states the detected mutants of the sample of a run that fails",
				[]string{record.Killed, record.Survived, record.NotRun},
				nil,
				&record.Sample{Before: "2", Detected: 1, Undetected: 1, Score: &half},
				"the run failed, 1 of a sample of 2 mutants detected (50%): 1 killed, 1 survived, 1 not run",
			},
			{
				"states a run that fails with an empty sample as a failed run",
				[]string{record.Suppressed, record.NotRun},
				nil,
				&record.Sample{Before: "1"},
				"the run failed: 1 suppressed, 1 not run",
			},
			{
				"states a run error as a failed run",
				nil,
				[]record.RunError{{Code: "load", Message: "x"}},
				nil,
				"the run failed",
			},
			{
				"states a run whose score counts no mutant",
				[]string{record.Suppressed},
				nil,
				nil,
				"no mutant to detect: 1 suppressed",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				r := full()
				r.Mutants, r.Errors, r.Sample, r.Generated = nil, tt.errors, tt.sample, nil
				for _, v := range tt.verdicts {
					r.Mutants = append(r.Mutants, record.Mutant{Verdict: v})
				}
				if got, want := r.Summary(spec.Load().Protocol), "github.com/google/btree: "+tt.want; got != want {
					t.Errorf("Summary() = %s\nwant        %s", got, want)
				}
			})
		}
		t.Run("states the generated files that the run leaves out with their mutants", func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				give []record.Generated
				want string
			}{
				{[]record.Generated{{File: "a_gen.go", Mutants: 1}}, "1 generated file with 1 mutant left out"},
				{
					[]record.Generated{{File: "a_gen.go", Mutants: 2}, {File: "b_gen.go", Mutants: 3}},
					"2 generated files with 5 mutants left out",
				},
			}
			for _, tt := range tests {
				r := full()
				r.Mutants, r.Generated = []record.Mutant{{Verdict: record.Killed}}, tt.give
				want := "github.com/google/btree: 1 of 1 mutant detected (100%): 1 killed, " + tt.want
				if got := r.Summary(spec.Load().Protocol); got != want {
					t.Errorf("Summary() = %s\nwant        %s", got, want)
				}
			}
		})
	})
	t.Run("FileName", func(t *testing.T) {
		t.Parallel()
		t.Run("escapes the import path as one path segment", func(t *testing.T) {
			t.Parallel()
			if got := record.FileName("github.com/google/btree"); got != "github.com%2Fgoogle%2Fbtree.mutate.json" {
				t.Errorf("FileName() = %s", got)
			}
		})
	})
	t.Run("Write", func(t *testing.T) {
		t.Parallel()
		t.Run("writes a record that the vendored schema accepts", func(t *testing.T) {
			t.Parallel()
			path, err := full().Write(filepath.Join(t.TempDir(), "records"))
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(path) != "github.com%2Fgoogle%2Fbtree.mutate.json" {
				t.Errorf("Write() wrote %s", path)
			}
			conforms(t, path)
		})
		t.Run("writes the sample of a run that the caller ended", func(t *testing.T) {
			t.Parallel()
			r := full()
			score := 1.0
			r.Mutants[1].Verdict = record.NotRun
			r.Sample = &record.Sample{Before: r.Mutants[1].Key, Detected: 1, Score: &score}
			path, err := r.Write(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			conforms(t, path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "\"sample\": {\n    \"before\": \"0123456789abcdef\",\n    \"detected\": 1,\n    \"undetected\": 0,\n    \"score\": 1\n  }"
			if !strings.Contains(string(data), want) {
				t.Errorf("the record lacks the sample %s:\n%s", want, data)
			}
		})
		t.Run(
			"writes null for an unmeasured ceiling, an unmeasured peak, no score, no sample, no selection and no suite",
			func(t *testing.T) {
				t.Parallel()
				r := full()
				r.Limits = []record.Limits{{Target: r.Target.Name, DeadlineSeconds: 4}}
				r.Control.Opening.PeakBytes, r.Selection, r.Control.Closing = nil, nil, nil
				r.Suite, r.Control.Ordinary, r.Mutants[0].Confirmed = nil, nil, false
				path, err := r.Write(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				conforms(t, path)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{
					`"memoryCeilingBytes": null`, `"peakBytes": null`, `"selection": null`, `"score": null`,
					`"sample": null`, `"suite": null`,
				} {
					if !strings.Contains(string(data), want) {
						t.Errorf("the record lacks %s:\n%s", want, data)
					}
				}
				for _, absent := range []string{`"closing"`, `"ordinary"`, `"confirmed"`} {
					if strings.Contains(string(data), absent) {
						t.Errorf("the record states %s, which the run did not have:\n%s", absent, data)
					}
				}
			},
		)
		t.Run("writes an empty list for a selection of no line of the package", func(t *testing.T) {
			t.Parallel()
			r := full()
			r.Selection = []record.Range{}
			path, err := r.Write(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			conforms(t, path)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"selection": []`) {
				t.Errorf("the record lacks an empty selection:\n%s", data)
			}
		})
		t.Run("returns an error when the directory does not exist and cannot be made", func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(file, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := full().Write(filepath.Join(file, "records")); err == nil ||
				!strings.HasPrefix(err.Error(), "record: ") {
				t.Errorf("Write() error = %v", err)
			}
		})
		t.Run("returns an error when the record file cannot be written", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, record.FileName("github.com/google/btree")), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := full().Write(dir); err == nil || !strings.HasPrefix(err.Error(), "record: ") {
				t.Errorf("Write() error = %v", err)
			}
		})
	})
	t.Run("EngineVersion", func(t *testing.T) {
		t.Parallel()
		module := func(version string, replace *debug.Module) *debug.Module {
			return &debug.Module{Path: record.Module, Version: version, Replace: replace}
		}
		tests := []struct {
			name string
			give *debug.BuildInfo
			want string
		}{
			{"returns (devel) without build information", nil, "(devel)"},
			{"returns the main module's version", &debug.BuildInfo{Main: *module("v0.1.0", nil)}, "v0.1.0"},
			{
				"returns the dependency's version",
				&debug.BuildInfo{
					Deps: []*debug.Module{{Path: "example.com/other", Version: "v9.9.9"}, module("v0.2.0", nil)},
				},
				"v0.2.0",
			},
			{
				"returns the replacement's version",
				&debug.BuildInfo{
					Deps: []*debug.Module{module("v0.2.0", &debug.Module{Path: "example.com/fork", Version: "v0.2.1"})},
				},
				"v0.2.1",
			},
			{
				"returns (devel) for a replacement by a directory",
				&debug.BuildInfo{Deps: []*debug.Module{module("v0.2.0", &debug.Module{Path: "../mutate"})}},
				"(devel)",
			},
			{
				"returns (devel) for a binary without the module",
				&debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}},
				"(devel)",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := record.EngineVersion(tt.give); got != tt.want {
					t.Errorf("EngineVersion() = %s, want %s", got, tt.want)
				}
			})
		}
	})
}

// TestSchemaCheck proves the schema check of these tests: each case breaks
// one rule of the schema in a valid record, and the check names it.
func TestSchemaCheck(t *testing.T) {
	t.Parallel()
	mutant := func(v map[string]any) map[string]any { return v["mutants"].([]any)[0].(map[string]any) }
	tests := []struct {
		name string
		give func(v, defs map[string]any)
		want string
	}{
		{"names a missing property", func(v, _ map[string]any) { delete(v, "inputs") }, "record: lacks inputs"},
		{
			"names a missing overlay version",
			func(v, _ map[string]any) { delete(v, "overlay") },
			"record: lacks overlay",
		},
		{"names an undeclared property", func(v, _ map[string]any) { v["extra"] = 1 }, "has the property extra"},
		{
			"names a wrong constant",
			func(v, _ map[string]any) { v["record"] = "other" },
			"record.record: other is not dokimi-mutate",
		},
		{
			"names a value outside an enumeration",
			func(v, _ map[string]any) { mutant(v)["verdict"] = "dead" },
			"is not in",
		},
		{"names a pattern mismatch", func(v, _ map[string]any) { v["inputs"] = "md5:x" }, "does not match"},
		{
			"names a wrong type",
			func(v, _ map[string]any) { v["toolchain"] = 7.0 },
			"record.toolchain: 7 is not of type string",
		},
		{
			"names a constant of another type",
			func(v, _ map[string]any) { v["version"] = "1" },
			"record.version: 1 is not 1",
		},
		{"names a fraction where an integer belongs", func(v, _ map[string]any) {
			v["control"].(map[string]any)["opening"].(map[string]any)["sites"] = 1.5
		}, "is not of type integer"},
		{
			"names a long original",
			func(v, _ map[string]any) { mutant(v)["original"] = strings.Repeat("é", 121) },
			"is longer than 120",
		},
		{"names an empty scope", func(v, _ map[string]any) { mutant(v)["scope"] = "" }, "is shorter than 1"},
		{"names a number below its minimum", func(v, _ map[string]any) { mutant(v)["seconds"] = -1.0 }, "is below 0"},
		{
			"names a number above its maximum",
			func(v, _ map[string]any) { v["score"] = 1.5 },
			"matches 0 of the oneOf schemas",
		},
		{
			"names a number at an exclusive minimum",
			func(v, _ map[string]any) { v["limits"].([]any)[0].(map[string]any)["deadlineSeconds"] = 0.0 },
			"is not above 0",
		},
		{
			"names a time that is not a date-time",
			func(v, _ map[string]any) { v["startedAt"] = "yesterday" },
			"is not a date-time",
		},
		{
			"names a keyword that it does not know",
			func(_, defs map[string]any) { defs["kind"].(map[string]any)["uniqueItems"] = true },
			"does not know",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			schema := schema(t)
			var v map[string]any
			data, err := json.Marshal(full())
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			defs := schema["$defs"].(map[string]any)
			if problems := validate(defs, schema, v, "record"); len(problems) != 0 {
				t.Fatalf("the unbroken record breaks the schema: %v", problems)
			}
			tt.give(v, defs)
			problems := strings.Join(validate(defs, schema, v, "record"), "\n")
			if !strings.Contains(problems, tt.want) {
				t.Errorf("problems:\n%s\nwant one that contains %q", problems, tt.want)
			}
		})
	}
}
