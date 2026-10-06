// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build go1.26

package enumerate_test

import "testing"

// Go 1.26 lets new take an expression, whose value the new variable holds.
func TestSitesOfGo126(t *testing.T) {
	t.Parallel()
	t.Run("Enumerate", func(t *testing.T) {
		t.Parallel()
		t.Run("makes a zero mutant of a return of the value that new(x) points to", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{
				"go.mod": "module fixture\n\ngo 1.26\n",
				"new.go": "package fixture\n\nfunc pointed(x int) (int, int) {\n\treturn *new(x), *new(int)\n}\n",
			})
			want(t, listing(r, "sbr-zero"), `pointed sbr-zero 0: return *new(x), *new(int) -> "return 0, 0"`+"\n")
		})
	})
}
