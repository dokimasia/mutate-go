// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import (
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/enumerate"
)

func TestKey(t *testing.T) {
	t.Parallel()
	t.Run("Key", func(t *testing.T) {
		t.Parallel()
		t.Run("hashes the fields separated by NUL bytes", func(t *testing.T) {
			t.Parallel()
			// Each digest was computed with printf and sha256sum, outside Go.
			tests := []struct {
				giveFile, giveScope, giveKind, giveTokens string
				giveOccurrence                            int
				want                                      string
			}{
				{"between.go", "between", "ror-boundary", "x < hi", 0, "ba74901e75daf19d"},
				{"steps.go", "steps", "ror-false", "n > 0", 1, "681ab23c877298d7"},
			}
			for _, tt := range tests {
				if got := enumerate.Key(
					"dokimi-mutate-key/1",
					tt.giveFile,
					tt.giveScope,
					tt.giveKind,
					tt.giveTokens,
					tt.giveOccurrence,
				); got != tt.want {
					t.Errorf(
						"Key(%q, %q, %q, %q, %d) = %s, want %s",
						tt.giveFile,
						tt.giveScope,
						tt.giveKind,
						tt.giveTokens,
						tt.giveOccurrence,
						got,
						tt.want,
					)
				}
			}
		})
	})
	t.Run("Tokens", func(t *testing.T) {
		t.Parallel()
		t.Run("drops comments and the semicolons that the scanner inserts", func(t *testing.T) {
			t.Parallel()
			tests := map[string]string{
				"a /* note */ +\n\tb":          "a + b",
				"if x {\n\ty = 1 // set\n}":    "if x { y = 1 }",
				"for i := 0; i < n; i++ {\n}":  "for i := 0 ; i < n ; i ++ { }",
				"s == \"a  b\"":                "s == \"a  b\"",
				"x<-y":                         "x <- y",
				"return *new(int), errors.New": "return * new ( int ) , errors . New",
			}
			for give, want := range tests {
				if got := enumerate.Tokens([]byte(give)); got != want {
					t.Errorf("Tokens(%q) = %q, want %q", give, got, want)
				}
			}
		})
	})
	t.Run("Cut", func(t *testing.T) {
		t.Parallel()
		a, b := strings.Repeat("a", 150), strings.Repeat("b", 200)
		tests := []struct {
			name                          string
			giveOriginal, giveReplacement string
			wantOriginal, wantReplacement string
		}{
			{
				"collapses each run of whitespace into one space",
				"if x {\n\t\ty = 1\n\t}", "x < 1\t\t&& y",
				"if x { y = 1 }", "x < 1 && y",
			},
			{
				"keeps fields of 120 characters whole",
				strings.Repeat("é", 120), strings.Repeat("ü", 120),
				strings.Repeat("é", 120), strings.Repeat("ü", 120),
			},
			{
				"keeps the first 119 characters of a deleted site and an ellipsis",
				strings.Repeat("é", 121), "",
				strings.Repeat("é", 119) + "…", "",
			},
			{
				"keeps the first characters when fewer than 40 precede the difference",
				"x < " + b, "x <= " + b,
				("x < " + b)[:119] + "…", ("x <= " + b)[:119] + "…",
			},
			{
				"keeps 40 characters before the difference and the rest of a short field",
				a + " && ok", a,
				"…" + a[:40] + " && ok", "…" + a[:40],
			},
			{
				"cuts a long window at both ends",
				a + " + " + b, a + " - " + b,
				"…" + (a + " + " + b)[111:229] + "…", "…" + (a + " - " + b)[111:229] + "…",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				original, replacement := enumerate.Cut(tt.giveOriginal, tt.giveReplacement)
				if original != tt.wantOriginal || replacement != tt.wantReplacement {
					t.Errorf(
						"Cut() = %q, %q, want %q, %q",
						original,
						replacement,
						tt.wantOriginal,
						tt.wantReplacement,
					)
				}
			})
		}
	})
}
