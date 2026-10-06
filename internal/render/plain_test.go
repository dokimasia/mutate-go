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
			bound := "package fixture\n\nfunc f(a, b int, ok bool) (_mutateZero0 int) {\n"
			want := map[string]string{
				"f sbr-delete 0":   head + "\tif (0 != 0) { if a < b &&\n\t\tok {\n\t\ta++\n\t} }\n\treturn -a\n}\n",
				"f lcr-left 0":     head + "\tif ((a < b) || (0 != 0) && \n(ok)) {\n" + tail,
				"f lcr-right 0":    head + "\tif ((0 != 0) && (a < b) || \n(ok)) {\n" + tail,
				"f lcr-false 0":    head + "\tif ((0 != 0) && ((a < b) && \n(ok))) {\n" + tail,
				"f ror-boundary 0": head + "\tif a <= b &&\n\t\tok {\n" + tail,
				"f ror-false 0":    head + "\tif (a < b && (0 != 0)) &&\n\t\tok {\n" + tail,
				"f uoi-incdec 0":   head + "\tif a < b &&\n\t\tok {\n\t\ta--\n\t}\n\treturn -a\n}\n",
				"f sbr-zero 0": bound + "\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n" +
					"\tif (0 == 0) { return _mutateZero0 }; return -a\n}\n",
				"f uoi-minus 0": head + "\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\treturn (a)\n}\n",
			}
			names := ids(r)
			path := filepath.Join(p.Dir, "f.go")
			got := map[string]string{}
			for _, m := range r.Mutants {
				got[names[m]] = string(render.Plain(p, m, prefix).Files[path])
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
				got[names[m]] = string(render.Plain(p, m, prefix).Files[path])
			}
			expect.Equal(t, got["g aor 0"], head+"\tn *= 3\n\treturn !ok == (n > 0)\n}\n",
				"aor writes the compound operator")
			expect.Equal(t, got["g uoi-not 0"], head+"\tn %= 3\n\treturn (ok) == (n > 0)\n}\n",
				"uoi-not of a negation drops the negation")
		})

		t.Run("binds a result whose type spans lines and keeps every line", func(t *testing.T) {
			t.Parallel()
			result := "struct {\n\ta int\n\tb int // the second field\n}"
			ret := "return struct {\n\t\ta int\n\t\tb int // the second field\n\t}{n, n}\n}\n"
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc f(n int) " + result + " {\n\t" + ret})
			assert.Length(t, r.Mutants, 1, "the return is the only site")
			got := string(render.Plain(p, r.Mutants[0], prefix).Files[filepath.Join(p.Dir, "f.go")])
			assert.Equal(t, got, "package fixture\n\nfunc f(n int) (_mutateZero0 "+result+") {\n"+
				"\tif (0 == 0) { return _mutateZero0 }; "+ret,
				"the result's type and the return keep their lines")
		})

		semantics := []struct {
			name string
			fx   semanticsFixture
		}{
			{name: "builds each mutant to compute what its instrumented form computes", fx: everyKind},
			{
				name: "builds each mutant to compute the constants true and false where the package hides their names",
				fx:   hiddenConstants,
			},
			{
				name: "builds each mutant to compute what its instrumented form computes where the package declares " +
					"the names of the instrumentation",
				fx: clashingNames,
			},
		}
		for _, tt := range semantics {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				p, r := fixture(t, tt.fx.files)
				prog := instrument(t, p, r)
				names := ids(r)
				for _, m := range r.Mutants {
					if m.Status != enumerate.Runnable {
						continue
					}
					t.Run(names[m], func(t *testing.T) {
						t.Parallel()
						bin := buildProgram(t, p, render.Plain(p, m, prog.Prefix))
						assert.Equal(
							t,
							tt.fx.describe(lines(run(t, bin, p.Dir))),
							tt.fx.describe(tt.fx.expected(names, m)),
							"the ordinary build of the mutant computes what the catalogue states",
						)
					})
				}
			})
		}
	})
}
