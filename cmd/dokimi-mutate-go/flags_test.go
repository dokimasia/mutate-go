// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/selection"
)

// suffixes are the suffixes of a size, by the power of 1024 that each
// states.
var suffixes = []string{"", "K", "M", "G", "T"}

func TestFlags(t *testing.T) {
	t.Parallel()

	t.Run("parse", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the defaults for a command line without a flag", func(t *testing.T) {
			t.Parallel()
			o, err := parse(nil, 1<<30)
			assert.NoError(t, err, "an empty command line is valid")
			assert.Equal(t, o, &options{parallel: 1, workers: 1, budget: 1 << 30, patterns: []string{currentPackage}},
				"the command tests the current package with one worker and the given budget")
		})

		t.Run("returns the value of each flag and the patterns", func(t *testing.T) {
			t.Parallel()
			o, err := parse([]string{
				"-" + flagLines, "a.go:1-2", "-" + flagLines, "b.go:3-3", "-" + flagDiff, "change.diff",
				"-" + flagSample, "5", "-" + flagIncludeGenerated, "-" + flagSuite, "./conformance",
				"-" + flagConfirm, "-" + flagDir, "dir", "-" + flagParallel, "2", "-" + flagWorkers, "3",
				"-" + flagTimeout, "30m", "-" + flagBudget, "16G", "-" + flagJSON, "-" + flagRecord, "out",
				"-" + flagVersion, "./wire", "./codec",
			}, 0)
			assert.NoError(t, err, "every flag parses")
			assert.Equal(t, o, &options{
				dir:              "dir",
				lines:            linesFlag{{Path: "a.go", First: 1, Last: 2}, {Path: "b.go", First: 3, Last: 3}},
				diff:             "change.diff",
				sample:           5,
				includeGenerated: true,
				suite:            suiteFlag{"./conformance"},
				confirm:          true,
				parallel:         2,
				workers:          3,
				timeout:          30 * time.Minute,
				budget:           16 << 30,
				json:             true,
				records:          "out",
				version:          true,
				patterns:         []string{"./wire", "./codec"},
			}, "the options state each flag's value")
		})

		t.Run("returns the listing flag", func(t *testing.T) {
			t.Parallel()
			o, err := parse([]string{"-" + flagList}, 0)
			assert.NoError(t, err, "-list alone is valid")
			assert.True(t, o.list, "the options state the listing")
		})

		t.Run("returns flag.ErrHelp for -h", func(t *testing.T) {
			t.Parallel()
			_, err := parse([]string{"-h"}, 0)
			assert.ErrorIs(t, err, flag.ErrHelp, "-h asks for the help")
		})

		tests := []struct {
			name string
			give []string
			want string
		}{
			{
				"returns an error for a flag that is not defined",
				[]string{"-unknown"},
				"flag provided but not defined: -unknown",
			},
			{
				"returns an error for a value that does not parse",
				[]string{"-" + flagLines, "a.go"},
				`invalid value "a.go" for flag -lines: selection: "a.go" is not file:first-last with 1 <= first <= last`,
			},
			{
				"returns an error for fewer than one package at once",
				[]string{"-" + flagParallel, "0"},
				"-p takes a number of at least 1",
			},
			{
				"returns an error for fewer than one worker",
				[]string{"-" + flagWorkers, "0"},
				"-workers takes a number of at least 1",
			},
			{
				"returns an error for a negative sample",
				[]string{"-" + flagSample, "-1"},
				"-sample takes a number of at least 0",
			},
			{
				"returns an error for a negative timeout",
				[]string{"-" + flagTimeout, "-1s"},
				"-timeout takes a duration of at least 0",
			},
			{
				"returns an error for a budget that is no size",
				[]string{"-" + flagBudget, "8E"},
				`invalid value "8E" for flag -memory-budget: "8E" is not a number of bytes with an optional K, M, G ` +
					`or T for a power of 1024`,
			},
			{
				"returns an error for a listing with JSON",
				[]string{"-" + flagList, "-" + flagJSON},
				"-list writes no record, so it does not take -json or -record",
			},
			{
				"returns an error for a listing with records",
				[]string{"-" + flagList, "-" + flagRecord, "out"},
				"-list writes no record, so it does not take -json or -record",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				_, err := parse(tt.give, 0)
				assert.HasError(t, err, "the command line is not valid")
				assert.Equal(t, err.Error(), tt.want, "the error states what is not valid")
			})
		}
	})

	t.Run("sizeFlag", func(t *testing.T) {
		t.Parallel()

		t.Run("Set", func(t *testing.T) {
			t.Parallel()

			t.Run("sets the number times the power of 1024 that its suffix states", func(t *testing.T) {
				t.Parallel()
				prop.ForAll(t, "a number and a suffix state the number times the suffix's power of 1024",
					func(c *prop.Case) {
						n := c.Draw(prop.Integer[int64](0, 1<<20), "n")
						power := c.Draw(prop.Integer(0, len(suffixes)-1), "power")
						var f sizeFlag
						assert.NoError(c, f.Set(strconv.FormatInt(n, 10)+suffixes[power]), "the size parses")
						assert.Equal(c, int64(f), n<<(10*power), "the size is the number of bytes")
						assert.Equal(c, f.String(), strconv.FormatInt(n<<(10*power), 10), "String states the bytes")
					})
			})

			for _, give := range []string{"", "G", "-1", "1.5G", "8E", "9000000000T"} {
				t.Run("returns an error for "+strconv.Quote(give), func(t *testing.T) {
					t.Parallel()
					var f sizeFlag
					assert.HasError(t, f.Set(give), "the value is no size")
				})
			}
		})
	})

	t.Run("linesFlag", func(t *testing.T) {
		t.Parallel()

		t.Run("Set", func(t *testing.T) {
			t.Parallel()

			t.Run("adds the lines of each entry with its path as the entry writes it", func(t *testing.T) {
				t.Parallel()
				var f linesFlag
				assert.NoError(t, f.Set("a.go:1-2"), "the first entry parses")
				assert.NoError(t, f.Set(filepath.Join("sub", "b.go")+":3-4"), "the second entry parses")
				assert.Equal(t, []selection.Lines(f), []selection.Lines{
					{Path: "a.go", First: 1, Last: 2},
					{Path: filepath.Join("sub", "b.go"), First: 3, Last: 4},
				}, "the flag collects both entries without resolving their paths")
				assert.Equal(t, f.String(), "", "the flag shows no value")
			})

			t.Run("returns the error of an entry that does not parse", func(t *testing.T) {
				t.Parallel()
				var f linesFlag
				assert.HasError(t, f.Set("a.go"), "an entry without a range does not parse")
				assert.Empty(t, f, "the flag keeps no entry")
			})
		})
	})

	t.Run("suiteFlag", func(t *testing.T) {
		t.Parallel()

		t.Run("Set", func(t *testing.T) {
			t.Parallel()

			t.Run("adds each pattern", func(t *testing.T) {
				t.Parallel()
				var f suiteFlag
				assert.NoError(t, f.Set("./a"), "a pattern is valid")
				assert.NoError(t, f.Set("./b/..."), "so is another")
				assert.Equal(t, f, suiteFlag{"./a", "./b/..."}, "the flag collects both patterns")
				assert.Equal(t, f.String(), "", "the flag shows no value")
			})
		})
	})
}
