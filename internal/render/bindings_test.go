// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/render"
)

func TestBindings(t *testing.T) {
	t.Parallel()

	t.Run("Render", func(t *testing.T) {
		t.Parallel()

		t.Run("names the results of each function that a return of zero values leaves", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\n" +
				"func f(n int) (int, error) {\n\treturn n, nil\n}\n\n" +
				"func g(n int) int {\n\th := func() (int) { return n }\n\treturn h()\n}\n"})
			prog := instrument(t, p, r)
			assert.Equal(t, string(prog.Files[filepath.Join(p.Dir, "f.go")]), "package fixture\n\n"+
				"func f(n int) (_mutateZero0 int, _mutateZero1 error) {\n"+
				"\tif _mutateIs(1) { return _mutateZero0, _mutateZero1 }; return n, nil\n}\n\n"+
				"func g(n int) (_mutateZero0 int) {\n"+
				"\th := func() (_mutateZero0 int) { if _mutateIs(2) { return _mutateZero0 }; return n }\n"+
				"\tif _mutateIs(3) { return _mutateZero0 }; return h()\n}\n",
				"each function and literal names its results, in parentheses where it has none")
		})

		t.Run("copies the named results before the first statement and renames a result named _", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\n" +
				"func f(n int) (_ int, err error) {return n, err}\n"})
			prog := instrument(t, p, r)
			assert.Equal(t, string(prog.Files[filepath.Join(p.Dir, "f.go")]), "package fixture\n\n"+
				"func f(n int) (_mutateZero0 int, err error) { _mutateZero1 := err;"+
				"if _mutateIs(1) { return _mutateZero0, _mutateZero1 }; return n, err}\n",
				"the copy precedes the return that starts the body")
		})
	})

	t.Run("Plain", func(t *testing.T) {
		t.Parallel()

		t.Run("binds the results of the function that the mutant's return leaves", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\n" +
				"func f(n int) (count int, _ error) {\n\tcount = n\n\treturn count, nil\n}\n"})
			names := ids(r)
			got := map[string]string{}
			for _, m := range r.Mutants {
				got[names[m]] = string(render.Plain(p, m, prefix).Files[filepath.Join(p.Dir, "f.go")])
			}
			assert.Equal(t, got["f sbr-zero 0"], "package fixture\n\n"+
				"func f(n int) (count int, _mutateZero1 error) { _mutateZero0 := count;\n\tcount = n\n"+
				"\tif (0 == 0) { return _mutateZero0, _mutateZero1 }; return count, nil\n}\n",
				"the ordinary build copies the named result and renames the result named _")
			assert.Equal(t, got["f sbr-delete 0"], "package fixture\n\n"+
				"func f(n int) (count int, _ error) {\n\tif (0 != 0) { count = n }\n\treturn count, nil\n}\n",
				"and binds nothing for another kind")
		})
	})
}
