// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/spec"
)

// keyPattern is a key as a record writes it.
const keyPattern = `^[0-9a-f]{16}$`

// maxText is the most characters that a record's original or replacement
// has.
const maxText = 120

// keyFields are the generated fields of a key.
type keyFields struct {
	Prefix, File, Scope, Tokens string
	Kind                        spec.Kind
	Occurrence                  int
}

// sourceText generates the text of a site: letters, spaces, tabs and line
// breaks, up to 300 characters.
var sourceText = prop.String(prop.Alphabet("ab \t\n"), prop.MaxSize(300))

func TestKey(t *testing.T) {
	t.Parallel()

	t.Run("Key", func(t *testing.T) {
		t.Parallel()

		// Each digest was computed with printf and sha256sum, outside Go.
		tests := []struct {
			name string
			give keyFields
			want string
		}{
			{
				name: "returns the digest of the fields of a first occurrence",
				give: keyFields{
					Prefix: "dokimi-mutate-key/1",
					File:   "between.go",
					Scope:  "between",
					Kind:   spec.RORBoundary,
					Tokens: "x < hi",
				},
				want: "ba74901e75daf19d",
			},
			{
				name: "returns the digest of the fields of a second occurrence",
				give: keyFields{
					Prefix:     "dokimi-mutate-key/1",
					File:       "steps.go",
					Scope:      "steps",
					Kind:       spec.RORFalse,
					Tokens:     "n > 0",
					Occurrence: 1,
				},
				want: "681ab23c877298d7",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				g := tt.give
				assert.Equal(t, enumerate.Key(g.Prefix, g.File, g.Scope, g.Kind, g.Tokens, g.Occurrence), tt.want,
					"Key is the start of the SHA-256 digest of the fields separated by NUL")
			})
		}

		t.Run("returns 16 hexadecimal digits for any fields", func(t *testing.T) {
			t.Parallel()
			prop.Matches(t, func(g keyFields) string {
				return enumerate.Key(g.Prefix, g.File, g.Scope, g.Kind, g.Tokens, g.Occurrence)
			}, keyPattern, "a key is 16 hexadecimal digits")
		})

		t.Run("returns two keys for two fields that join to one text", func(t *testing.T) {
			t.Parallel()
			assert.NotEqual(
				t,
				enumerate.Key("p", "ab", "c", spec.AOR, "x", 0),
				enumerate.Key("p", "a", "bc", spec.AOR, "x", 0),
				"a separator between the fields keeps their boundaries",
			)
		})
	})

	t.Run("Tokens", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name, give, want string
		}{
			{name: "leaves out a comment", give: "a /* note */ +\n\tb", want: "a + b"},
			{
				name: "leaves out a line comment and the inserted semicolons",
				give: "if x {\n\ty = 1 // set\n}",
				want: "if x { y = 1 }",
			},
			{
				name: "keeps the semicolons that the source writes",
				give: "for i := 0; i < n; i++ {\n}",
				want: "for i := 0 ; i < n ; i ++ { }",
			},
			{name: "keeps the text of a string literal", give: "s == \"a  b\"", want: "s == \"a  b\""},
			{name: "separates an operator from its operands", give: "x<-y", want: "x <- y"},
			{
				name: "writes a keyword and a selector token by token",
				give: "return *new(int), errors.New",
				want: "return * new ( int ) , errors . New",
			},
		}
		for _, tt := range tests {
			t.Run("returns the tokens of a site that "+tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(
					t,
					enumerate.Tokens([]byte(tt.give)),
					tt.want,
					"Tokens joins the scanner's tokens by single spaces",
				)
			})
		}

		t.Run("returns the same tokens whatever separates them", func(t *testing.T) {
			t.Parallel()
			token := prop.SampledFrom(
				"a", "b1", "_x", "42", "0x1F", "3.5", `"s  t"`, "'c'", "+", "-", "*", "/", "%", "<<", "&^", "&&",
				"||", "<-", "++", "==", "!=", "<=", ":=", "...", "(", ")", "[", "]", "{", "}", ",", ";", ".",
			)
			separator := prop.SampledFrom(" ", "\t", "  ", "\n", " /* c */ ", " // c\n")
			prop.ForAll(t, "whitespace and comments between tokens do not change a key's tokens", func(c *prop.Case) {
				tokens := c.Draw(prop.List(token, prop.MinSize(1), prop.MaxSize(12)), "tokens")
				var src strings.Builder
				for i, tok := range tokens {
					if i > 0 {
						src.WriteString(c.Draw(separator, "separator"))
					}
					src.WriteString(tok)
				}
				assert.Equal(
					c,
					enumerate.Tokens([]byte(src.String())),
					strings.Join(tokens, " "),
					"Tokens returns the tokens alone",
				)
			})
		})
	})

	t.Run("Cut", func(t *testing.T) {
		t.Parallel()

		a, b := strings.Repeat("a", 150), strings.Repeat("b", 200)
		tests := []struct {
			name                          string
			original, replacement         string
			wantOriginal, wantReplacement string
		}{
			{
				name:     "returns both fields with each run of whitespace as one space",
				original: "if x {\n\t\ty = 1\n\t}", replacement: "x < 1\t\t&& y",
				wantOriginal: "if x { y = 1 }", wantReplacement: "x < 1 && y",
			},
			{
				name:     "returns two fields of 120 characters as they are",
				original: strings.Repeat("é", 120), replacement: strings.Repeat("ü", 120),
				wantOriginal: strings.Repeat("é", 120), wantReplacement: strings.Repeat("ü", 120),
			},
			{
				name:     "returns the first 119 characters of a deleted site of 121",
				original: strings.Repeat("é", 121), replacement: "",
				wantOriginal: strings.Repeat("é", 119) + "…", wantReplacement: "",
			},
			{
				name:     "returns the start of two fields that differ in their first 40 characters",
				original: "x < " + b, replacement: "x <= " + b,
				wantOriginal: ("x < " + b)[:119] + "…", wantReplacement: ("x <= " + b)[:119] + "…",
			},
			{
				name:     "returns the 40 characters before a difference at the end",
				original: a + " && ok", replacement: a,
				wantOriginal: "…" + a[:40] + " && ok", wantReplacement: "…" + a[:40],
			},
			{
				name:            "returns one window around a difference in the middle",
				original:        a + " + " + b,
				replacement:     a + " - " + b,
				wantOriginal:    "…" + (a + " + " + b)[111:229] + "…",
				wantReplacement: "…" + (a + " - " + b)[111:229] + "…",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				original, replacement := enumerate.Cut(tt.original, tt.replacement)
				expect.Equal(t, original, tt.wantOriginal, "the original is cut")
				expect.Equal(t, replacement, tt.wantReplacement, "and the replacement keeps the same window")
			})
		}

		t.Run("returns fields of at most 120 characters that differ where the sites differ", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "a cut fits the record and never hides the change of a mutant", cut)
		})
	})
}

// FuzzKey runs the property of Cut on inputs of the fuzzer.
func FuzzKey(f *testing.F) {
	prop.Fuzz(f, "a cut fits the record and never hides the change of a mutant", cut)
}

// cut is the property of Cut: each field has at most 120 characters, and
// two sites that differ beyond their whitespace give two fields that
// differ.
func cut(c *prop.Case) {
	o, r := c.Draw(sourceText, "original"), c.Draw(sourceText, "replacement")
	original, replacement := enumerate.Cut(o, r)
	assert.InRange(c, utf8.RuneCountInString(original), 0, maxText, "the original fits the record")
	assert.InRange(c, utf8.RuneCountInString(replacement), 0, maxText, "and so does the replacement")
	if strings.Join(strings.Fields(o), " ") != strings.Join(strings.Fields(r), " ") {
		assert.NotEqual(c, original, replacement, "the two fields differ where the sites differ")
	}
}
