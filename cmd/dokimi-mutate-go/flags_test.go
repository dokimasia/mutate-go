// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"math"
	"strconv"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"
	gocli "go.stealthscale.io/cli"

	"go.dokimi.dev/mutate/internal/selection"
)

// suffixes are the suffixes of a size, by the power of 1024 that each
// states.
var suffixes = []string{"", "K", "M", "G", "T"}

// historyProblem is the problem of --history beside an option that selects
// lines or lists the mutants.
const historyProblem = "--history gives every mutant a verdict, so it does not take --lines, --diff or --list"

// fromVariable returns the source of every option in the cases of problem,
// an environment variable.
func fromVariable(any) (gocli.Source, bool) {
	return gocli.Source{Kind: gocli.Environment, Name: optionVarPrefix + "VALUE"}, true
}

func TestFlags(t *testing.T) {
	t.Parallel()

	t.Run("options", func(t *testing.T) {
		t.Parallel()

		t.Run("problem", func(t *testing.T) {
			t.Parallel()

			t.Run("returns no problem for the defaults", func(t *testing.T) {
				t.Parallel()
				o := &options{Parallel: 1, Workers: 1}
				assert.Equal(t, o.problem(fromVariable), "",
					"the command runs with one package and one worker at a time")
			})

			tests := []struct {
				name string
				give options
				want string
			}{
				{
					"returns the problem of fewer than one package at once",
					options{Parallel: 0, Workers: 1},
					`invalid --parallel "0" from env DOKIMI_MUTATE_GO_VALUE: want a number of at least 1`,
				},
				{
					"returns the problem of fewer than one worker",
					options{Parallel: 1, Workers: 0},
					`invalid --workers "0" from env DOKIMI_MUTATE_GO_VALUE: want a number of at least 1`,
				},
				{
					"returns the problem of a negative sample",
					options{Parallel: 1, Workers: 1, Sample: -1},
					`invalid --sample "-1" from env DOKIMI_MUTATE_GO_VALUE: want a number of at least 0`,
				},
				{
					"returns the problem of a negative timeout",
					options{Parallel: 1, Workers: 1, Timeout: -time.Second},
					`invalid --timeout "-1s" from env DOKIMI_MUTATE_GO_VALUE: want a duration of at least 0`,
				},
				{
					"returns the problem of a listing with JSON",
					options{Parallel: 1, Workers: 1, List: true, JSON: true},
					"--list writes no record, so it does not take --json or --record",
				},
				{
					"returns the problem of a listing with records",
					options{Parallel: 1, Workers: 1, List: true, Record: "out"},
					"--list writes no record, so it does not take --json or --record",
				},
				{
					"returns the problem of a history with lines",
					options{Parallel: 1, Workers: 1, History: "history", Lines: []selection.Lines{{Path: "a.go"}}},
					historyProblem,
				},
				{
					"returns the problem of a history with a diff",
					options{Parallel: 1, Workers: 1, History: "history", Diff: "change.diff"},
					historyProblem,
				},
				{
					"returns the problem of a history with a listing",
					options{Parallel: 1, Workers: 1, History: "history", List: true},
					historyProblem,
				},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					assert.Equal(t, tt.give.problem(fromVariable), tt.want,
						"the problem names the option, its value, its source and its range")
				})
			}
		})
	})

	t.Run("size", func(t *testing.T) {
		t.Parallel()

		t.Run("UnmarshalText", func(t *testing.T) {
			t.Parallel()

			t.Run("sets the number times the power of 1024 that its suffix states", func(t *testing.T) {
				t.Parallel()
				prop.ForAll(t, "a number and a suffix state the number times the suffix's power of 1024",
					func(c *prop.Case) {
						n := c.Draw(prop.Integer[int64](0, 1<<20), "n")
						power := c.Draw(prop.Integer(0, len(suffixes)-1), "power")
						var s size
						assert.NoError(c, s.UnmarshalText([]byte(strconv.FormatInt(n, 10)+suffixes[power])),
							"the size parses")
						assert.Equal(c, int64(s), n<<(10*power), "the size is the number of bytes")
					})
			})

			t.Run("sets the largest size of each suffix that fits in an int64", func(t *testing.T) {
				t.Parallel()
				for power, suffix := range suffixes {
					largest := int64(math.MaxInt64) >> (10 * power)
					var s size
					expect.NoError(t, s.UnmarshalText([]byte(strconv.FormatInt(largest, 10)+suffix)), "the size parses")
					expect.Equal(t, int64(s), largest<<(10*power), "the size is the number of bytes")
				}
			})

			for _, give := range []string{"", "G", "-1", "1.5G", "8E", "9000000000T"} {
				t.Run("returns an error for "+strconv.Quote(give), func(t *testing.T) {
					t.Parallel()
					var s size
					err := s.UnmarshalText([]byte(give))
					assert.HasError(t, err, "the value is no size")
					assert.Equal(t, err.Error(), sizeForm, "the error states the form of a size")
				})
			}
		})
	})
}
