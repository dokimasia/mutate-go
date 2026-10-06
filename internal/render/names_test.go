// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/render"
)

// nextPrefix starts each name of the instrumentation in a package that has
// an identifier that starts with prefix, and none that starts with
// nextPrefix.
const nextPrefix = "_mutate1"

// claimed is a file of the package fixture that declares a name that the
// instrumentation declares in a package without such a name. It has no
// site.
const claimed = "package fixture\n\nvar _mutateIs = 0\n"

// missing is the name of a file that no fixture has.
const missing = "missing"

func TestNames(t *testing.T) {
	t.Parallel()

	t.Run("Render", func(t *testing.T) {
		t.Parallel()

		t.Run("starts each name with _mutate where no identifier of the package starts with it", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			assert.Equal(t, instrument(t, p, r).Prefix, prefix, "the prefix is _mutate")
		})

		t.Run("takes the first prefix with which no identifier starts", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":     used["f.go"],
				"names.go": "package fixture\n\n// _mutate2 is in a comment.\nvar _mutateA, _mutate1B = \"_mutate2\", 0\n",
			})
			assert.Equal(t, instrument(t, p, r).Prefix, "_mutate2",
				"the identifiers rule out _mutate and _mutate1, and the comment and the string rule out nothing")
		})

		t.Run("starts no name with a prefix of an identifier of a test file", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":            used["f.go"],
				"f_test.go":       used["f_test.go"],
				"claimed_test.go": claimed,
			})
			prog := instrument(t, p, r)
			assert.Equal(t, prog.Prefix, nextPrefix, "the test file's identifier rules out _mutate")
			bin := buildProgram(t, p, prog)
			assert.Equal(t, lines(run(t, bin, p.Dir, active(0)))["Used"], "2",
				"the test binary builds and computes as the source")
		})

		t.Run("renames each name of the instrumentation and no other text", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, everyKind.files)
			want := instrument(t, p, r)
			files := maps.Clone(everyKind.files)
			files["claimed.go"] = claimed
			q, s := fixture(t, files)
			got := instrument(t, q, s)
			assert.Equal(t, got.Prefix, nextPrefix, "the declaration rules out _mutate")
			assert.Length(t, got.Files, len(want.Files), "the programs have the same files")
			for path, text := range want.Files {
				name := filepath.Base(path)
				expect.Equal(t, string(got.Files[filepath.Join(q.Dir, name)]),
					strings.ReplaceAll(string(text), prefix, nextPrefix),
					name+" differs from the program of the package without the declaration in the prefix alone")
			}
		})

		t.Run("returns an error when a test file does not read", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			assert.NoError(t, os.Symlink(filepath.Join(p.Dir, missing), filepath.Join(p.Dir, "broken_test.go")),
				"a link to no file is the test file")
			_, err := render.Render(p, r, mutantVar)
			assert.HasError(t, err, "Render fails")
			assert.HasPrefix(t, err.Error(), "render: ", "the error starts with the package's name")
		})

		t.Run("returns an error when the package directory does not exist", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, used)
			p.Dir = filepath.Join(p.Dir, missing)
			_, err := render.Render(p, r, mutantVar)
			assert.HasError(t, err, "Render fails")
			assert.HasPrefix(t, err.Error(), "render: ", "the error starts with the package's name")
		})
	})
}
