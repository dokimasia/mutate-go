// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/run"
)

// sumFile is the new version of a file of the diffs below, one line per
// line number.
const sumFile = "package fixture\n\nfunc sum(a, b int) int {\n\ttotal := a\n\ttotal += b\n\treturn total\n}\n"

// diffFiles writes each of files into a new directory and returns the
// absolute path of each, by name.
func diffFiles(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for name, text := range files {
		paths[name] = filepath.Join(dir, name)
		if err := os.WriteFile(paths[name], []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// ranges writes one line per range of a selection: its file's base name
// and its lines.
func ranges(lines []run.Lines) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%s:%d-%d\n", filepath.Base(l.Path), l.First, l.Last)
	}
	return b.String()
}

func TestDiff(t *testing.T) {
	t.Parallel()
	t.Run("ParseDiff", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			// give is the diff, with %[1]s for the path of sum.go and %[2]s for
			// the path of other.go.
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
				"--- /dev/null\n+++ b/%[1]s\n@@ -0,0 +1,7 @@\n+package fixture\n+\n+func sum(a, b int) int {\n+\ttotal := a\n+\ttotal += b\n+\treturn total\n+}\n",
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
				"+++ %[2]s\n@@ -1 +1 @@\n-package old\n+package fixture\n+++ %[1]s\n@@ -1,7 +1,7 @@\n-package old\n+package fixture\n \n func sum(a, b int) int {\n-\ttotal := b\n+\ttotal := a\n \ttotal += b\n \treturn total\n }\n",
				"other.go:1-1\nsum.go:1-1\nsum.go:3-4\n",
			},
			{
				"reads a quoted path and drops a timestamp after a tab",
				"+++ %[3]s\t2026-10-06 08:00:00.000000000 +0000\n@@ -5 +5 @@\n-\ttotal -= b\n+\ttotal += b\n",
				"sum.go:4-5\n",
			},
			{
				"skips the note of a missing final line break",
				"+++ %[2]s\n@@ -1 +1 @@\n-package old\n\\ No newline at end of file\n+package fixture\n\\ No newline at end of file\n",
				"other.go:1-1\n",
			},
			{
				"reads an empty line of a hunk as a context line",
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
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				paths := diffFiles(t, map[string]string{"sum.go": sumFile, "other.go": "package fixture"})
				text := fmt.Sprintf(tt.give, paths["sum.go"], paths["other.go"], strconv.Quote("b/"+paths["sum.go"]))
				got, err := run.ParseDiff(text)
				if err != nil || got == nil || ranges(got) != tt.want {
					t.Errorf("ParseDiff() = %q, %v, want %q", ranges(got), err, tt.want)
				}
			})
		}
		t.Run("returns a path after b/ relative to the working directory", func(t *testing.T) {
			t.Parallel()
			wd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			got, err := run.ParseDiff("+++ b/sub/gone.go\n@@ -2,1 +1,0 @@\n-\treturn\n")
			if err != nil || len(got) != 1 ||
				got[0] != (run.Lines{Path: filepath.Join(wd, "sub", "gone.go"), First: 1, Last: 1}) {
				t.Errorf("ParseDiff() = %+v, %v", got, err)
			}
		})
		t.Run("returns an error for a diff that does not parse or does not match the files", func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				name, give, want string
			}{
				{"older than the file", "+++ %[1]s\n@@ -5 +5 @@\n-\ttotal += b\n+\ttotal -= b\n", "states line 5 of"},
				{"past the end of the file", "+++ %[1]s\n@@ -8,0 +9 @@\n+// more\n", "states line 9 of"},
				{"a hunk before a header", "@@ -1 +1 @@\n-a\n+b\n", "before a file's header"},
				{"a header without ranges", "+++ %[1]s\n@@ -1 @@\n", "does not parse"},
				{"a header without its closing marks", "+++ %[1]s\n@@ -1 +1 @ x\n", "does not parse"},
				{"a header without a minus", "+++ %[1]s\n@@ 1 +1 @@\n", "does not parse"},
				{"a header without a plus", "+++ %[1]s\n@@ -1 1 @@\n", "does not parse"},
				{"an old range that is not a number", "+++ %[1]s\n@@ -x +1 @@\n", "does not parse"},
				{"a new count that is not a number", "+++ %[1]s\n@@ -1 +1,y @@\n", "does not parse"},
				{"a negative count", "+++ %[1]s\n@@ -1,-1 +1 @@\n", "does not parse"},
				{
					"a diff that ends inside a hunk",
					"+++ %[1]s\n@@ -1,2 +1,2 @@\n package fixture",
					"ends inside the hunk",
				},
				{"a line without a prefix", "+++ %[1]s\n@@ -1 +1 @@\n*package fixture\n", "has no line's prefix"},
				{"a file that does not read", "+++ %[1]s.missing\n@@ -1 +1 @@\n package fixture\n", "no such file"},
			}
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					paths := diffFiles(t, map[string]string{"sum.go": sumFile})
					_, err := run.ParseDiff(fmt.Sprintf(tt.give, paths["sum.go"]))
					if err == nil || !strings.HasPrefix(err.Error(), "run: ") ||
						!strings.Contains(err.Error(), tt.want) {
						t.Errorf("ParseDiff() error = %v, want one that contains %q", err, tt.want)
					}
				})
			}
		})
	})
}
