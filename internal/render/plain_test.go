// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/render"
)

func TestPlain(t *testing.T) {
	t.Parallel()

	t.Run("Plain", func(t *testing.T) {
		t.Parallel()

		t.Run("writes each mutant's source on the lines of the original", func(t *testing.T) {
			t.Parallel()
			head := "package fixture\n\nfunc f(a, b int, ok bool) int {\n"
			src := head + "\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\treturn -a\n}\n"
			p, r := fixture(t, map[string]string{"f.go": src})
			tail := "\t\ta++\n\t}\n\treturn -a\n}\n"
			want := map[string]string{
				"f sbr-delete 0":   head + "\tif false { if a < b &&\n\t\tok {\n\t\ta++\n\t} }\n\treturn -a\n}\n",
				"f lcr-left 0":     head + "\tif ((a < b) || false && \n(ok)) {\n" + tail,
				"f lcr-right 0":    head + "\tif (false && (a < b) || \n(ok)) {\n" + tail,
				"f lcr-false 0":    head + "\tif (false && ((a < b) && \n(ok))) {\n" + tail,
				"f ror-boundary 0": head + "\tif a <= b &&\n\t\tok {\n" + tail,
				"f ror-false 0":    head + "\tif (a < b && false) &&\n\t\tok {\n" + tail,
				"f uoi-incdec 0":   head + "\tif a < b &&\n\t\tok {\n\t\ta--\n\t}\n\treturn -a\n}\n",
				"f sbr-zero 0":     head + "\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\tif true { return 0 }; return -a\n}\n",
				"f uoi-minus 0":    head + "\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\treturn (a)\n}\n",
			}
			names := ids(r)
			path := filepath.Join(p.Dir, "f.go")
			got := map[string]string{}
			for _, m := range r.Mutants {
				got[names[m]] = string(render.Plain(p, m).Files[path])
			}
			assert.Equal(t, got, want, "each mutant's source keeps the lines of the original")
		})

		t.Run("writes the operator of a compound assignment and the negation of an operand", func(t *testing.T) {
			t.Parallel()
			head := "package fixture\n\nfunc g(n int, ok bool) bool {\n"
			p, r := fixture(t, map[string]string{"f.go": head + "\tn %= 3\n\treturn !ok == (n > 0)\n}\n"})
			names := ids(r)
			path := filepath.Join(p.Dir, "f.go")
			got := map[string]string{}
			for _, m := range r.Mutants {
				got[names[m]] = string(render.Plain(p, m).Files[path])
			}
			expect.Equal(t, got["g aor 0"], head+"\tn *= 3\n\treturn !ok == (n > 0)\n}\n",
				"aor writes the compound operator")
			expect.Equal(t, got["g uoi-not 0"], head+"\tn %= 3\n\treturn (ok) == (n > 0)\n}\n",
				"uoi-not of a negation drops the negation")
		})

		t.Run("builds each mutant to compute what its instrumented form computes", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, semanticsFiles())
			instrument(t, p, r)
			names := ids(r)
			for _, m := range r.Mutants {
				if m.Status != enumerate.Runnable {
					continue
				}
				t.Run(names[m], func(t *testing.T) {
					t.Parallel()
					bin := buildProgram(t, p, render.Plain(p, m))
					assert.Equal(t, describe(lines(run(t, bin, p.Dir))), describe(expected(names, m)),
						"the ordinary build of the mutant computes what the catalogue states")
				})
			}
		})
	})
}
