// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package selection_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/selection"
)

// fileName generates a relative path of a few parts, with the colons that
// a path may contain.
var fileName = prop.String(prop.Alphabet("ab:/"), prop.MinSize(1), prop.MaxSize(8)).Filter(func(name string) bool {
	return !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, ":") && filepath.Clean(name) == name
})

// line generates a line number.
var line = prop.Integer(1, 10000)

func TestLines(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	assert.NoError(t, err, "the working directory reads")

	t.Run("ParseEntry", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the absolute path and the range of an entry", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(
				t,
				"an entry file:first-last parses to the file's absolute path and the range",
				func(c *prop.Case) {
					file := c.Draw(fileName, "file")
					first := c.Draw(line, "first")
					last := c.Draw(prop.Integer(first, first+100), "last")
					got, err := selection.ParseEntry(fmt.Sprintf("%s:%d-%d", file, first, last))
					assert.NoError(c, err, "a range from line 1 or later to its start or later parses")
					assert.Equal(c, got, selection.Lines{Path: filepath.Join(wd, file), First: first, Last: last},
						"the file ends at the entry's last colon")
				},
			)
		})

		t.Run("returns the path of an absolute file as it is", func(t *testing.T) {
			t.Parallel()
			got, err := selection.ParseEntry("/abs/x:y.go:4-4")
			assert.NoError(t, err, "an absolute file parses")
			assert.Equal(t, got, selection.Lines{Path: "/abs/x:y.go", First: 4, Last: 4}, "the path keeps its colon")
		})

		tests := []struct {
			name string
			give string
		}{
			{"returns an error for an entry without a range", "a.go"},
			{"returns an error for an entry without a file", ":1-2"},
			{"returns an error for a range without a last line", "a.go:1"},
			{"returns an error for a first line that is not a number", "a.go:x-2"},
			{"returns an error for a last line that is not a number", "a.go:1-y"},
			{"returns an error for a first line below 1", "a.go:0-2"},
			{"returns an error for a last line before the first", "a.go:5-4"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				_, err := selection.ParseEntry(tt.give)
				assert.HasError(t, err, "the entry does not parse")
				assert.Equal(
					t,
					err.Error(),
					fmt.Sprintf("selection: %q is not file:first-last with 1 <= first <= last", tt.give),
					"the error states the entry and its form",
				)
			})
		}
	})

	t.Run("ParseList", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the lines of each entry in order", func(t *testing.T) {
			t.Parallel()
			got, err := selection.ParseList("a.go:5-5,sub/b.go:6-9")
			assert.NoError(t, err, "the list parses")
			assert.Equal(t, got, []selection.Lines{
				{Path: filepath.Join(wd, "a.go"), First: 5, Last: 5},
				{Path: filepath.Join(wd, "sub", "b.go"), First: 6, Last: 9},
			}, "each entry is a range in the list's order")
		})

		t.Run("returns no selection for an empty list", func(t *testing.T) {
			t.Parallel()
			got, err := selection.ParseList("")
			assert.NoError(t, err, "an empty list parses")
			expect.Nil(t, got, "and selects every line")
		})

		t.Run("returns the error of the first entry that does not parse", func(t *testing.T) {
			t.Parallel()
			_, err := selection.ParseList("a.go:5-5,a.go,b.go")
			assert.HasError(t, err, "the list does not parse")
			assert.Contains(t, err.Error(), `"a.go" is not file:first-last`, "the error states the entry")
		})
	})
}
