// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// schemaPath is the path of the vendored record schema, relative to the
// package's directory.
var schemaPath = filepath.Join("..", "..", "conformance", "spec", "record.schema.json")

// The pins of the record file's name and of the start of the package's
// errors.
const (
	fileName    = "github.com%2Fgoogle%2Fbtree.mutate.json"
	errorPrefix = "record: "
)

// The modes of the files and the directories that the cases make.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// protocol is the run protocol of the vendored definition.
var protocol = spec.Load().Protocol

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
			Key: "3f9a0c71d2b4e816", Kind: spec.LCRFalse, File: "btree.go", Scope: "items.find",
			Start: record.Position{Line: 218, Column: 5}, End: record.Position{Line: 218, Column: 33},
			Original: "i > 0 && !less(s[i-1], item)", Replacement: "false", Verdict: spec.Killed,
			Tests: []string{"TestBTreeG"}, CoveredBy: []string{"TestBTreeG", "TestDescendRangeG"},
			Seconds: &seconds, Confirmed: true,
		}, {
			Key: "0123456789abcdef", Kind: spec.SBRDelete, File: "btree.go", Scope: "items.find",
			Start: record.Position{Line: 3, Column: 2}, End: record.Position{Line: 3, Column: 9},
			Original: "f(x)", Replacement: "", Verdict: spec.Suppressed, Rule: "logging",
		}},
		Skipped: []record.Skip{{
			File:   "btree.go",
			Start:  record.Position{Line: 136, Column: 36},
			End:    record.Position{Line: 136, Column: 41},
			Reason: "operand of type-parameter type",
		}},
		Generated: []record.Generated{{File: "btree_gen.go", Mutants: 12}},
	}
}

// keyed returns a record of mutants with the keys and the verdicts of
// pairs, in that order.
func keyed(pairs ...any) *record.Record {
	r := full()
	r.Mutants = nil
	for i := 0; i < len(pairs); i += 2 {
		r.Mutants = append(r.Mutants, record.Mutant{Key: pairs[i].(string), Verdict: pairs[i+1].(spec.Verdict)})
	}
	return r
}

func TestRecord(t *testing.T) {
	t.Parallel()

	t.Run("Failed", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			give func(r *record.Record)
			want bool
		}{
			{"reports false for a run with a verdict of every mutant", func(*record.Record) {}, false},
			{
				"reports true for a run error",
				func(r *record.Record) { r.Errors = []record.RunError{{Code: spec.ErrorBuild, Message: "x"}} },
				true,
			},
			{
				"reports true for a mutant that did not run",
				func(r *record.Record) { r.Mutants[1].Verdict = spec.NotRun },
				true,
			},
			{
				"reports true for a mutant whose run ended in an error",
				func(r *record.Record) { r.Mutants[1].Verdict = spec.Error },
				true,
			},
			{
				"reports false for a mutant that did not run beyond the caller's limit on the mutant runs",
				func(r *record.Record) {
					r.Mutants[1].Verdict = spec.NotRun
					r.Sample = &record.Sample{Before: r.Mutants[1].Key, Limit: new(1)}
				},
				false,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				r := full()
				tt.give(r)
				assert.Equal(t, r.Failed(), tt.want, "a run error, an error and an unlimited not-run fail the run")
			})
		}
	})

	t.Run("SetScore", func(t *testing.T) {
		t.Parallel()

		t.Run("divides the detected mutants by the detected and the undetected ones", func(t *testing.T) {
			t.Parallel()
			r := keyed("0", spec.Killed, "1", spec.TimedOut, "2", spec.Exhausted, "3", spec.Survived,
				"4", spec.NoCoverage, "5", spec.NotViable, "6", spec.Suppressed, "7", spec.NotSelected,
				"8", spec.Killed, "9", spec.Survived)
			r.SetScore(protocol, 0)
			assert.Equal(t, r.Score, new(4.0/7.0), "four of the seven counted mutants are detected")
		})

		t.Run("sets no score for a run that fails", func(t *testing.T) {
			t.Parallel()
			r := keyed("0", spec.Killed, "1", spec.Error)
			r.SetScore(protocol, 0)
			assert.Nil(t, r.Score, "a run with a mutant in error has no score")
		})

		t.Run("sets no score for a run without a detected or an undetected mutant", func(t *testing.T) {
			t.Parallel()
			r := keyed("0", spec.Suppressed, "1", spec.NotSelected)
			r.Score = new(1.0)
			r.SetScore(protocol, 0)
			assert.Nil(t, r.Score, "a score of no counted mutant does not exist")
		})

		t.Run("samples the mutants whose keys sort before the least key of a not-run mutant", func(t *testing.T) {
			t.Parallel()
			r := keyed("5", spec.NotRun, "1", spec.Killed, "4", spec.Killed, "3", spec.NotRun,
				"2", spec.Survived, "0", spec.Suppressed)
			r.SetScore(protocol, 0)
			assert.Equal(t, r.Sample, &record.Sample{Before: "3", Detected: 1, Undetected: 1, Score: new(0.5)},
				"the keys 0 to 2 are the sample")
			assert.Nil(t, r.Score, "a run that a deadline ended has no score")
		})

		t.Run("states the sample of each run with a not-run mutant", func(t *testing.T) {
			t.Parallel()
			var verdicts []spec.Verdict
			for _, v := range protocol.Verdicts {
				verdicts = append(verdicts, v.ID)
			}
			list := prop.List(prop.SampledFrom(verdicts...), prop.MinSize(1), prop.MaxSize(12))
			prop.ForAll(t, "the sample counts the mutants before the least not-run key", func(c *prop.Case) {
				drawn := c.Draw(list, "verdicts")
				var slots []int
				for i := range drawn {
					slots = append(slots, i)
				}
				order := c.Draw(prop.Permutation(slots...), "order")
				r, before := full(), ""
				r.Mutants = nil
				for i, v := range drawn {
					key := fmt.Sprintf("%02d", order[i])
					r.Mutants = append(r.Mutants, record.Mutant{Key: key, Verdict: v})
					if v == spec.NotRun && (before == "" || key < before) {
						before = key
					}
				}
				r.SetScore(protocol, 0)
				if before == "" {
					assert.Nil(c, r.Sample, "a run without a not-run mutant has no sample")
					return
				}
				assert.NotNil(c, r.Sample, "a run with a not-run mutant has a sample")
				assert.Equal(c, r.Sample.Before, before, "the sample ends at the least key of a not-run mutant")
				detected, undetected := r.Tally(protocol, before)
				assert.Equal(c, [2]int{r.Sample.Detected, r.Sample.Undetected}, [2]int{detected, undetected},
					"the sample counts the mutants before that key")
				assert.Nil(c, r.Score, "a run with a not-run mutant and no limit has no score")
			})
		})

		t.Run("sets the caller's limit and the sample's score as the run's score", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", spec.Killed, "2", spec.Survived, "3", spec.NotRun, "4", spec.NoCoverage)
			r.SetScore(protocol, 2)
			want := &record.Sample{Before: "3", Limit: new(2), Detected: 1, Undetected: 1, Score: new(0.5)}
			assert.Equal(t, r.Sample, want, "the sample states the caller's limit")
			assert.False(t, r.Failed(), "a run that the limit ended does not fail")
			assert.Equal(t, r.Score, new(0.5), "the run's score is the sample's")
		})

		t.Run("sets a sample without a score when no sampled mutant counts", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", spec.NotRun, "0", spec.Suppressed, "2", spec.Killed)
			r.SetScore(protocol, 0)
			assert.Equal(t, r.Sample, &record.Sample{Before: "1"}, "the sample before the key 1 counts no mutant")
		})

		t.Run("sets no sample for a run with a run error", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", spec.Killed, "2", spec.NotRun)
			r.Errors = []record.RunError{{Code: spec.ErrorBuild, Message: "x"}}
			r.SetScore(protocol, 1)
			assert.Nil(t, r.Sample, "a run error voids the sample")
			assert.Nil(t, r.Score, "and the score")
		})

		t.Run("sets no sample for a run in which every mutant has a verdict", func(t *testing.T) {
			t.Parallel()
			r := keyed("1", spec.Killed, "2", spec.Error)
			r.Sample = &record.Sample{Before: "2"}
			r.SetScore(protocol, 0)
			assert.Nil(t, r.Sample, "a run without a not-run mutant has no sample")
		})
	})

	t.Run("Tally", func(t *testing.T) {
		t.Parallel()

		t.Run("counts each verdict as the protocol classifies it", func(t *testing.T) {
			t.Parallel()
			for _, v := range protocol.Verdicts {
				detected, undetected := keyed("0", v.ID).Tally(protocol, "")
				want := map[spec.ScoreClass][2]int{spec.Detected: {1, 0}, spec.Undetected: {0, 1}}[v.Score]
				assert.Equal(t, [2]int{detected, undetected}, want, "the verdict counts in its score class alone")
			}
		})

		t.Run("counts only the mutants whose keys sort before the bound", func(t *testing.T) {
			t.Parallel()
			detected, undetected := keyed("0", spec.Killed, "1", spec.Survived, "2", spec.Killed).Tally(protocol, "2")
			assert.Equal(t, [2]int{detected, undetected}, [2]int{1, 1}, "the key 2 is outside the tally")
		})
	})

	t.Run("Path", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		assert.NoError(t, err, "the root resolves")
		assert.NoError(t, os.Mkdir(filepath.Join(root, "pkg"), dirMode), "the package's directory is made")
		link := filepath.Join(t.TempDir(), "link")
		assert.NoError(t, os.Symlink(filepath.Join(root, "pkg"), link), "the link is made")
		r := &record.Record{Root: root}
		tests := []struct {
			name, file, dir, want string
		}{
			{"returns the file relative to a directory through a link", "pkg/a.go", link, "a.go"},
			{
				"returns the file relative to a directory",
				"b.go", filepath.Join(root, "pkg"), filepath.Join("..", "b.go"),
			},
			{
				"returns the file relative to a directory that does not exist",
				"b.go", filepath.Join(root, "missing"), filepath.Join("..", "b.go"),
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, r.Path(tt.file, tt.dir), tt.want, "the path leads from the directory to the file")
			})
		}
	})

	t.Run("FileName", func(t *testing.T) {
		t.Parallel()

		t.Run("escapes the import path as one path segment", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, record.FileName("github.com/google/btree"), fileName, "the name contains no separator")
		})
	})

	t.Run("Write", func(t *testing.T) {
		t.Parallel()

		t.Run("writes a record that the vendored schema accepts", func(t *testing.T) {
			t.Parallel()
			path, err := full().Write(filepath.Join(t.TempDir(), "records"))
			assert.NoError(t, err, "the record writes")
			assert.Equal(t, filepath.Base(path), fileName, "the file has the record's name")
			conforms(t, read(t, path))
		})

		t.Run("writes the sample of a run that the caller ended", func(t *testing.T) {
			t.Parallel()
			r := full()
			r.Mutants[1].Verdict = spec.NotRun
			r.Sample = &record.Sample{Before: r.Mutants[1].Key, Detected: 1, Score: new(1.0)}
			path, err := r.Write(t.TempDir())
			assert.NoError(t, err, "the record writes")
			data := read(t, path)
			conforms(t, data)
			want := "\"sample\": {\n    \"before\": \"0123456789abcdef\",\n    \"limit\": null,\n" +
				"    \"detected\": 1,\n    \"undetected\": 0,\n    \"score\": 1\n  }"
			assert.Contains(t, string(data), want, "the record states the sample")
		})

		t.Run("writes null for each measure that the run does not have", func(t *testing.T) {
			t.Parallel()
			r := full()
			r.Limits = []record.Limits{{Target: r.Target.Name, DeadlineSeconds: 4}}
			r.Control.Opening.PeakBytes, r.Selection, r.Control.Closing = nil, nil, nil
			r.Suite, r.Control.Ordinary, r.Mutants[0].Confirmed = nil, nil, false
			path, err := r.Write(t.TempDir())
			assert.NoError(t, err, "the record writes")
			data := string(read(t, path))
			conforms(t, []byte(data))
			for _, want := range []string{
				`"memoryCeilingBytes": null`, `"peakBytes": null`, `"selection": null`, `"score": null`,
				`"sample": null`, `"suite": null`,
			} {
				assert.Contains(t, data, want, "the record states the measure as null")
			}
			for _, absent := range []string{`"closing"`, `"ordinary"`, `"confirmed"`} {
				assert.NotContains(t, data, absent, "the record leaves out what the run did not have")
			}
		})

		t.Run("writes an empty list for a selection of no line of the package", func(t *testing.T) {
			t.Parallel()
			r := full()
			r.Selection = []record.Range{}
			path, err := r.Write(t.TempDir())
			assert.NoError(t, err, "the record writes")
			data := read(t, path)
			conforms(t, data)
			assert.Contains(t, string(data), `"selection": []`, "the record states the empty selection")
		})

		t.Run("returns an error when the directory does not exist and cannot be made", func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "file")
			assert.NoError(t, os.WriteFile(file, nil, fileMode), "the file in the directory's place is written")
			_, err := full().Write(filepath.Join(file, "records"))
			assert.HasError(t, err, "a directory under a file cannot be made")
			assert.HasPrefix(t, err.Error(), errorPrefix, "the error starts with the package's name")
		})

		t.Run("returns an error when the record file cannot be written", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			assert.NoError(t, os.Mkdir(filepath.Join(dir, fileName), dirMode), "a directory takes the file's name")
			_, err := full().Write(dir)
			failed := assert.ErrorAs[*fs.PathError](t, err, "the error is the error of the file")
			assert.Equal(t, filepath.Base(failed.Path), fileName, "the error names the record file")
			assert.HasPrefix(t, err.Error(), errorPrefix, "the error starts with the package's name")
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
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, record.EngineVersion(tt.give), tt.want, "the version identifies the engine's module")
			})
		}
	})
}

// TestSchemaCheck proves the schema check of these tests: each case breaks
// one rule of the schema in a record that conforms, and the check rejects
// the record with a problem that names the rule.
func TestSchemaCheck(t *testing.T) {
	t.Parallel()

	t.Run("conforms", func(t *testing.T) {
		t.Parallel()
		mutant := func(v map[string]any) map[string]any { return v["mutants"].([]any)[0].(map[string]any) }
		tests := []struct {
			name string
			give func(v, defs map[string]any)
			want string
		}{
			{"rejects a missing property", func(v, _ map[string]any) { delete(v, "inputs") }, "record: lacks inputs"},
			{
				"rejects a missing overlay version",
				func(v, _ map[string]any) { delete(v, "overlay") },
				"record: lacks overlay",
			},
			{"rejects an undeclared property", func(v, _ map[string]any) { v["extra"] = 1 }, "has the property extra"},
			{
				"rejects a wrong constant",
				func(v, _ map[string]any) { v["record"] = "other" },
				"record.record: other is not dokimi-mutate",
			},
			{
				"rejects a value outside an enumeration",
				func(v, _ map[string]any) { mutant(v)["verdict"] = "dead" },
				"is not in",
			},
			{"rejects a pattern mismatch", func(v, _ map[string]any) { v["inputs"] = "md5:x" }, "does not match"},
			{
				"rejects a wrong type",
				func(v, _ map[string]any) { v["toolchain"] = 7.0 },
				"record.toolchain: 7 is not of type string",
			},
			{
				"rejects a constant of another type",
				func(v, _ map[string]any) { v["version"] = "1" },
				"record.version: 1 is not 1",
			},
			{"rejects a fraction where an integer belongs", func(v, _ map[string]any) {
				v["control"].(map[string]any)["opening"].(map[string]any)["sites"] = 1.5
			}, "is not of type integer"},
			{
				"rejects a long original",
				func(v, _ map[string]any) { mutant(v)["original"] = strings.Repeat("é", 121) },
				"is longer than 120",
			},
			{"rejects an empty scope", func(v, _ map[string]any) { mutant(v)["scope"] = "" }, "is shorter than 1"},
			{
				"rejects a number below its minimum",
				func(v, _ map[string]any) { mutant(v)["seconds"] = -1.0 },
				"is below 0",
			},
			{
				"rejects a number above its maximum",
				func(v, _ map[string]any) { v["score"] = 1.5 },
				"matches 0 of the oneOf schemas",
			},
			{
				"rejects a number at an exclusive minimum",
				func(v, _ map[string]any) { v["limits"].([]any)[0].(map[string]any)["deadlineSeconds"] = 0.0 },
				"is not above 0",
			},
			{
				"rejects a time that is not a date-time",
				func(v, _ map[string]any) { v["startedAt"] = "yesterday" },
				"is not a date-time",
			},
			{
				"rejects a boolean where a string belongs",
				func(v, _ map[string]any) { mutant(v)["file"] = true },
				"is not of type string",
			},
			{
				"rejects a keyword that the check does not know",
				func(_, defs map[string]any) { defs["kind"].(map[string]any)["uniqueItems"] = true },
				"does not know",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				data, err := json.Marshal(full())
				assert.NoError(t, err, "the record encodes")
				conforms(t, data)
				var v map[string]any
				assert.NoError(t, json.Unmarshal(data, &v), "the record decodes")
				schema := decodeSchema(t)
				defs := schema["$defs"].(map[string]any)
				tt.give(v, defs)
				failures := assert.Rejects(t, "the check rejects the broken record", func(tb assert.TB) {
					assert.Equal(tb, validate(defs, schema, v, "record"), nil, "the record conforms to the schema")
				})
				assert.Length(t, failures, 1, "the check fails once")
				problems, _ := failures[0].Detail["got"].([]string)
				assert.Contains(t, strings.Join(problems, "\n"), tt.want, "a problem names the broken rule")
			})
		}
	})
}

// read returns the contents of the file at path.
func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.NoError(t, err, "the record file reads")
	return data
}

// decodeSchema returns the vendored record schema, decoded.
func decodeSchema(t *testing.T) map[string]any {
	t.Helper()
	var s map[string]any
	assert.NoError(t, json.Unmarshal(read(t, schemaPath), &s), "the schema decodes")
	return s
}

// conforms stops the test when the record data breaks the vendored record
// schema, and states each problem.
func conforms(t *testing.T, data []byte) {
	t.Helper()
	schema := decodeSchema(t)
	var v any
	assert.NoError(t, json.Unmarshal(data, &v), "the record decodes")
	problems := validate(schema["$defs"].(map[string]any), schema, v, "record")
	slices.Sort(problems)
	assert.Equal(t, problems, nil, "the record conforms to the schema")
}

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
			if !slices.Contains(rule.([]any), v) {
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

// isType reports whether the decoded JSON value v has the schema type t.
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
	case "boolean":
		_, ok := v.(bool)
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
