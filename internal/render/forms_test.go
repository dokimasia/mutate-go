// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/enumerate"
)

// blank returns spaces in place of each byte of line.
func blank(line string) string { return strings.Repeat(" ", len(line)) }

func TestForms(t *testing.T) {
	t.Parallel()

	t.Run("Render", func(t *testing.T) {
		t.Parallel()

		t.Run("writes each form without a line of its own", func(t *testing.T) {
			t.Parallel()
			src := "package fixture\n\nfunc f(a, b int, ok bool) int {\n\tif a < b &&\n\t\tok {\n\t\ta++\n\t}\n\treturn -a\n}\n"
			p, r := fixture(t, map[string]string{"f.go": src})
			prog := instrument(t, p, r)
			assert.Equal(
				t,
				string(prog.Files[filepath.Join(p.Dir, "f.go")]),
				"package fixture\n\nfunc f(a, b int, ok bool) int {\n"+
					"\tif !_mutateIs(1) { if (!_mutateCT(2, 4) && (_mutateActive == 3 || (_mutate_s5[int](a, b))) && \n"+
					"(_mutateActive == 2 || (ok))) {\n"+
					"\t\tif _mutateIs(7) { a-- } else { a++ }\n"+
					"\t} }\n"+
					"\tif _mutateIs(8) { return 0 }; return _mutate_s9[int](a)\n}\n",
				"each form keeps the lines of the code that it replaces",
			)
		})

		t.Run("raises the language version of a module below go 1.18 and keeps every line", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				goMod: "module fixture\n\ngo 1.16\n",
				"a.go": "//go:build go1.17 && !nonexistent\n// +build go1.17,!nonexistent\n\npackage fixture\n\n" +
					"func A(x int) int { return x + 1 }\n",
				"b.go": "// Copyright notice.\n\n//go:build go1.20\n\npackage fixture\n\nfunc B(x int) int { return x * 2 }\n",
				"c.go": "package fixture\n\n// C returns x minus one.\nfunc C(x int) int { return x - 1 }\n",
				"f_test.go": "package fixture\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\n" +
					"func TestPrint(t *testing.T) { fmt.Println(\"Sum\", A(1)+B(2)+C(3)) }\n",
			})
			prog, bin := build(t, p, r)
			a, b := filepath.Join(p.Dir, "a.go"), filepath.Join(p.Dir, "b.go")
			constraints := blank("//go:build go1.17 && !nonexistent") + "\n" + blank("// +build go1.17,!nonexistent")
			expect.HasPrefix(t, string(prog.Files[a]),
				"//go:build go1.18\n//line "+a+":1:1\n"+constraints+"\n\npackage fixture\n",
				"a file's constraint rises to go1.18 and its own lines are blanked")
			wantB := "//go:build go1.20\n//line " + b + ":1:1\n// Copyright notice.\n\n" +
				blank("//go:build go1.20") + "\n\npackage fixture\n"
			expect.HasPrefix(t, string(prog.Files[b]), wantB, "a file that requires a later version keeps it")
			expect.HasPrefix(t, string(prog.Files[filepath.Join(p.Dir, helperFile)]),
				"//go:build go1.18\n\npackage fixture\n", "the helper file requires go1.18")
			assert.Equal(t, lines(run(t, bin, p.Dir, active(0)))["Sum"], "8", "the package computes as its source")
		})

		t.Run("writes a zero value whose type spans lines on one line", func(t *testing.T) {
			t.Parallel()
			src := "package fixture\n\nfunc f(n int) struct {\n\ta int\n\tb int // the second field\n} {\n" +
				"\treturn struct {\n\t\ta int\n\t\tb int // the second field\n\t}{n, n}\n}\n"
			p, r := fixture(t, map[string]string{"f.go": src})
			prog := instrument(t, p, r)
			assert.Length(t, r.Mutants, 1, "the return is the only site")
			expect.Equal(t, r.Mutants[0].Status, enumerate.Runnable, "the type checker accepts its form")
			got := string(prog.Files[filepath.Join(p.Dir, "f.go")])
			expect.Contains(t, got, "\tif _mutateIs(1) { return struct { a int ; b int ; } { } }; return struct {\n",
				"the form writes the semicolons of the zero value's type")
			expect.Equal(t, strings.Count(got, "\n"), strings.Count(src, "\n"), "and keeps every line")
		})
	})
}
