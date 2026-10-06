// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package selection_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/selection"
)

// sumFile is the new version of a file of the diffs below, one line per
// line number.
const sumFile = "package fixture\n\nfunc sum(a, b int) int {\n\ttotal := a\n\ttotal += b\n\treturn total\n}\n"

// fileMode is the mode of the files that the tests write.
const fileMode = 0o644

// diffFiles writes each of files into a new directory and returns the
// absolute path of each, by name.
func diffFiles(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for name, text := range files {
		paths[name] = filepath.Join(dir, name)
		assert.NoError(t, os.WriteFile(paths[name], []byte(text), fileMode), name+" is written")
	}
	return paths
}

// ranges writes one line per range of a selection: its file's base name
// and its lines.
func ranges(lines []selection.Lines) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%s:%d-%d\n", filepath.Base(l.Path), l.First, l.Last)
	}
	return b.String()
}

// numbers returns each line number that a selection of one file selects,
// in order.
func numbers(lines []selection.Lines) []int {
	var out []int
	for _, l := range lines {
		for n := l.First; n <= l.Last; n++ {
			out = append(out, n)
		}
	}
	return out
}

func TestDiff(t *testing.T) {
	t.Parallel()

	t.Run("ParseDiff", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			// give is the diff, with %[1]s for the path of sum.go, %[2]s for the
			// path of other.go, and %[3]s for the quoted path of sum.go after b/.
			give string
			want string
		}{
			{
				"returns the lines that a hunk adds",
				"--- a/sum.go\n+++ b/%[1]s\n@@ -3,3 +3,4 @@\n func sum(a, b int) int {\n \ttotal := a\n+\ttotal += b\n \treturn total\n",
				"sum.go:5-5\n",
			},
			{
				"returns the lines beside a run of removed lines",
				"--- a/sum.go\n+++ b/%[1]s\n@@ -4,5 +4,3 @@\n \ttotal := a\n-\ttotal -= b\n-\ttotal *= 2\n \ttotal += b\n \treturn total\n",
				"sum.go:4-5\n",
			},
			{
				"returns the line before removed lines that end a hunk without context",
				"--- a/sum.go\n+++ b/%[1]s\n@@ -6,2 +5,0 @@\n-\ttotal *= 2\n-\ttotal *= 3\n",
				"sum.go:5-5\n",
			},
			{
				"returns no line before removed lines at the top of a file",
				"--- a/sum.go\n+++ b/%[1]s\n@@ -1,1 +0,0 @@\n-// a comment\n",
				"",
			},
			{
				"returns every line of a new file",
				"--- /dev/null\n+++ b/%[1]s\n@@ -0,0 +1,7 @@\n+package fixture\n+\n+func sum(a, b int) int {\n" +
					"+\ttotal := a\n+\ttotal += b\n+\treturn total\n+}\n",
				"sum.go:1-7\n",
			},
			{
				"returns no line of a deleted file",
				"--- a/gone.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-package fixture\n-\n",
				"",
			},
			{
				"returns no line of a deleted file whose hunk states a new start",
				"--- a/gone.go\n+++ /dev/null\n@@ -3,2 +2,0 @@\n-\treturn\n-}\n",
				"",
			},
			{
				"returns the ranges by file in the order of the headers",
				"+++ %[2]s\n@@ -1 +1 @@\n-package old\n+package fixture\n+++ %[1]s\n@@ -1,7 +1,7 @@\n-package old\n" +
					"+package fixture\n \n func sum(a, b int) int {\n-\ttotal := b\n+\ttotal := a\n \ttotal += b\n \treturn total\n }\n",
				"other.go:1-1\nsum.go:1-1\nsum.go:3-4\n",
			},
			{
				"reads a quoted path and drops a timestamp after a tab",
				"+++ %[3]s\t2026-10-06 08:00:00.000000000 +0000\n@@ -5 +5 @@\n-\ttotal -= b\n+\ttotal += b\n",
				"sum.go:4-5\n",
			},
			{
				"skips the note of a missing final line break",
				"+++ %[2]s\n@@ -1 +1 @@\n-package old\n\\ No newline at end of file\n+package fixture\n" +
					"\\ No newline at end of file\n",
				"other.go:1-1\n",
			},
			{
				"reads an empty line of a hunk as a line that both versions have",
				"+++ %[1]s\n@@ -1,3 +1,3 @@\n package fixture\n\n-func sum(b, a int) int {\n+func sum(a, b int) int {\n",
				"sum.go:2-3\n",
			},
			{
				"returns no line of a diff without a hunk",
				"diff --git a/logo.png b/logo.png\nBinary files a/logo.png and b/logo.png differ\n",
				"",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				paths := diffFiles(t, map[string]string{"sum.go": sumFile, "other.go": "package fixture"})
				text := fmt.Sprintf(tt.give, paths["sum.go"], paths["other.go"], strconv.Quote("b/"+paths["sum.go"]))
				got, err := selection.ParseDiff(text)
				assert.NoError(t, err, "the diff parses")
				assert.NotNil(t, got, "a diff selects an empty list where it selects no line")
				assert.Equal(t, ranges(got), tt.want, "the diff selects the lines of the new versions")
			})
		}

		t.Run("returns the lines that a hunk adds and no other", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			prop.ForAll(t, "a hunk that adds lines selects exactly the added lines", func(c *prop.Case) {
				old := c.Draw(prop.List(prop.SampledFrom("a", "b", "c"), prop.MinSize(1), prop.MaxSize(8)), "old")
				adds := c.Draw(prop.List(prop.Boolean(), prop.MinSize(len(old)), prop.MaxSize(len(old))), "adds")
				var newLines, hunk []string
				var want []int
				for i, l := range old {
					newLines, hunk = append(newLines, l), append(hunk, " "+l)
					if adds[i] {
						newLines, hunk = append(newLines, "new"), append(hunk, "+new")
						want = append(want, len(newLines))
					}
				}
				f, err := os.CreateTemp(dir, "new-*.go")
				assert.NoError(c, err, "the new version's file is created")
				_, err = f.WriteString(strings.Join(newLines, "\n") + "\n")
				assert.NoError(c, err, "the new version is written")
				assert.NoError(c, f.Close(), "the new version's file closes")
				diff := fmt.Sprintf("+++ b/%s\n@@ -1,%d +1,%d @@\n%s\n", f.Name(), len(old), len(newLines),
					strings.Join(hunk, "\n"))
				got, err := selection.ParseDiff(diff)
				assert.NoError(c, err, "the diff matches the new version")
				assert.Equal(c, numbers(got), want, "the diff selects the added lines and no other",
					assert.EquateEmpty())
			})
		})

		t.Run("returns a path after b/ relative to the working directory", func(t *testing.T) {
			t.Parallel()
			wd, err := os.Getwd()
			assert.NoError(t, err, "the working directory reads")
			got, err := selection.ParseDiff("+++ b/sub/gone.go\n@@ -2,1 +1,0 @@\n-\treturn\n")
			assert.NoError(t, err, "a diff that removes a line reads no file")
			assert.Equal(t, got, []selection.Lines{{Path: filepath.Join(wd, "sub", "gone.go"), First: 1, Last: 1}},
				"the path is the working directory's")
		})

		errs := []struct {
			name, give, want string
		}{
			{
				"returns an error for a diff older than the file",
				"+++ %[1]s\n@@ -5 +5 @@\n-\ttotal += b\n+\ttotal -= b\n",
				"states line 5 of",
			},
			{
				"returns an error for a diff past the end of the file", "+++ %[1]s\n@@ -8,0 +9 @@\n+// more\n",
				"states line 9 of",
			},
			{"returns an error for a hunk before a header", "@@ -1 +1 @@\n-a\n+b\n", "before a file's header"},
			{"returns an error for a header without ranges", "+++ %[1]s\n@@ -1 @@\n", "does not parse"},
			{"returns an error for a header without its closing marks", "+++ %[1]s\n@@ -1 +1 @ x\n", "does not parse"},
			{"returns an error for a header without a minus", "+++ %[1]s\n@@ 1 +1 @@\n", "does not parse"},
			{"returns an error for a header without a plus", "+++ %[1]s\n@@ -1 1 @@\n", "does not parse"},
			{"returns an error for an old range that is not a number", "+++ %[1]s\n@@ -x +1 @@\n", "does not parse"},
			{"returns an error for a new count that is not a number", "+++ %[1]s\n@@ -1 +1,y @@\n", "does not parse"},
			{"returns an error for a negative count", "+++ %[1]s\n@@ -1,-1 +1 @@\n", "does not parse"},
			{
				"returns an error for a new range of lines at line 0",
				"+++ %[1]s\n@@ -0,0 +0,1 @@\n+a\n",
				"does not parse",
			},
			{
				"returns an error for an old range of lines at line 0",
				"+++ %[1]s\n@@ -0 +1 @@\n-a\n+b\n",
				"does not parse",
			},
			{
				"returns an error for a diff that ends inside a hunk", "+++ %[1]s\n@@ -1,2 +1,2 @@\n package fixture",
				"ends inside the hunk",
			},
			{
				"returns an error for a line without a prefix", "+++ %[1]s\n@@ -1 +1 @@\n*package fixture\n",
				"has no line's prefix",
			},
			{
				"returns an error for a file that does not read", "+++ %[1]s.missing\n@@ -1 +1 @@\n package fixture\n",
				"no such file",
			},
		}
		for _, tt := range errs {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				paths := diffFiles(t, map[string]string{"sum.go": sumFile})
				_, err := selection.ParseDiff(fmt.Sprintf(tt.give, paths["sum.go"]))
				assert.HasError(t, err, "the diff does not parse or does not match the file")
				assert.That(t, err.Error()).
					HasPrefix("selection: ", "the error starts with the package's name").
					Contains(tt.want, "and states the cause")
			})
		}
	})
}
