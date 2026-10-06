// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/golden"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/record"
)

// The files that state the help, relative to the package's directory: the
// command's documentation, and the module's README, which states the help
// between its markers.
const (
	docFile    = "doc.go"
	readmeFile = "../../README.md"
)

// The markers of the help in the README.
const (
	helpStart = "<!-- help:start -->\n\n```text\n"
	helpEnd   = "```\n\n<!-- help:end -->"
)

// docHeader starts doc.go before its package comment. The linters read
// doc.go, so the header states its source without the marker of a
// generated file, which they skip.
const docHeader = "// Copyright ThesmOS B.V. 2026\n// SPDX-License-Identifier: MIT\n\n" +
	"// TestHelp writes this file from the help with -update. Edit the help in help.go.\n\n"

// docSubject starts the package comment, before the help's first word.
const docSubject = "Command "

// word generates a word of a few letters, or of the tie that joins two.
var word = prop.String(prop.Alphabet("abcdefghijklmnopqrstuvwxyz"+tie), prop.MinSize(1), prop.MaxSize(12))

func TestHelp(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	help(&b, nil)
	text := b.String()

	t.Run("help", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the help that doc.go states", func(t *testing.T) {
			t.Parallel()
			golden.MatchAt(t, docFile, []byte(documentation(text)), golden.ShouldUpdate())
		})

		t.Run("writes the help that the README states between its markers", func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(readmeFile)
			assert.NoError(t, err, "the README reads")
			before, rest, found := strings.Cut(string(data), helpStart)
			assert.True(t, found, "the README marks the start of the help")
			_, after, found := strings.Cut(rest, helpEnd)
			assert.True(t, found, "the README marks the end of the help")
			golden.MatchAt(t, readmeFile, []byte(before+helpStart+text+helpEnd+after), golden.ShouldUpdate())
		})

		t.Run("lists each flag of the table in the table's order", func(t *testing.T) {
			t.Parallel()
			var flags []string
			for _, opt := range commandFlags {
				flags = append(flags, "\n"+flagIndent+"-"+opt.name)
			}
			assert.ContainsInOrder(t, text, flags, "the help lists every flag")
		})

		t.Run("appends a computed default to its flag's description", func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			help(&b, map[string]string{flagBudget: budgetDefault(9 << 30)})
			assert.Contains(t, strings.Join(strings.Fields(b.String()), " "),
				"0 sets no budget. Here the default is 9.0 GiB.", "the help states the default of this process")
		})

		t.Run("writes each tie as a space", func(t *testing.T) {
			t.Parallel()
			assert.That(t, text).
				NotContains(tie, "the help shows no tie").
				Contains("a + b to a - b.", "the tied words share a line")
		})
	})

	t.Run("budgetDefault", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			bytes int64
			want  string
		}{
			{"states the budget in GiB", 9 << 30, "Here the default is 9.0" + tie + "GiB."},
			{
				"states a budget of 0 for a process without a memory limit", 0,
				"Here the default is 0, because no file states the memory that the process may use.",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, budgetDefault(tt.bytes), tt.want, "the sentence states the default")
			})
		}
	})

	t.Run("version", func(t *testing.T) {
		t.Parallel()

		t.Run("writes the versions of the engine, the catalogue, the overlay and the toolchain", func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			version(&b)
			info, _ := debug.ReadBuildInfo()
			assert.Equal(t, b.String(), name+" "+record.EngineVersion(info)+"\ncatalogue "+definition.Version+
				"\noverlay "+definition.Overlay.Version+"\ntoolchain "+runtime.Version()+"\n",
				"each line states a name and its version")
		})
	})

	t.Run("wrap", func(t *testing.T) {
		t.Parallel()

		t.Run("breaks text at its spaces into lines of at most the width", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "the lines keep the words in order, and each line is at most the width or one word",
				func(c *prop.Case) {
					words := c.Draw(prop.List(word, prop.MinSize(1), prop.MaxSize(30)), "words")
					width := c.Draw(prop.Integer(1, 40), "width")
					var b strings.Builder
					wrap(&b, strings.Join(words, " "), usageIndent, width)
					var units, got []string
					for _, w := range words {
						units = append(units, strings.ReplaceAll(w, tie, " "))
					}
					for line := range strings.Lines(b.String()) {
						assert.HasPrefix(c, line, usageIndent, "each line starts with the indentation")
						body := strings.TrimSuffix(strings.TrimPrefix(line, usageIndent), "\n")
						if len(body) > width {
							assert.Contains(c, units, body, "a line longer than the width is one word")
						}
						got = append(got, body)
					}
					assert.Equal(c, strings.Join(got, " "), strings.Join(units, " "),
						"the lines keep every word in order, with each tie as a space")
				})
		})
	})
}

// documentation returns doc.go as the help text states it: the header, the
// help as the package comment with docSubject before its first word, and
// the package clause.
func documentation(text string) string {
	var b strings.Builder
	b.WriteString(docHeader)
	for line := range strings.SplitSeq(strings.TrimSuffix(docSubject+text, "\n"), "\n") {
		switch {
		case line == "":
			b.WriteString("//\n")
		case strings.HasPrefix(line, flagIndent):
			b.WriteString("//" + line + "\n")
		default:
			b.WriteString("// " + line + "\n")
		}
	}
	b.WriteString("package main\n")
	return b.String()
}
