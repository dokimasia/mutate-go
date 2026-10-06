// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/load"
)

// listError generates an error that go list states of a package: letters
// and the white space that go list writes around an error's text.
var listError = prop.String(prop.Alphabet("ab \t\n"), prop.MaxSize(8)).Map(func(text string) *load.ListError {
	return &load.ListError{Err: text}
})

func TestList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("List", func(t *testing.T) {
		t.Parallel()

		t.Run("lists each package that the patterns name with its directory", func(t *testing.T) {
			t.Parallel()
			dir := resolved(
				t,
				moduleDir(t, map[string]string{"a.go": "package fixture\n", "sub/b.go": "package sub\n"}),
			)
			got, err := load.List(ctx, load.Config{Dir: dir, Env: os.Environ()}, []string{"./..."})
			assert.NoError(t, err, "go list lists the module's packages")
			assert.Equal(t, got, []load.Listed{
				{ImportPath: "fixture", Dir: dir},
				{ImportPath: "fixture/sub", Dir: filepath.Join(dir, "sub")},
			}, "each entry states a package's import path and its directory")
		})

		t.Run("states the error of a pattern that names no directory in its entry", func(t *testing.T) {
			t.Parallel()
			dir := moduleDir(t, map[string]string{"a.go": "package fixture\n"})
			got, err := load.List(ctx, load.Config{Dir: dir, Env: os.Environ()}, []string{"./missing"})
			assert.NoError(t, err, "go list lists a pattern that names no directory")
			assert.Length(t, got, 1, "the pattern has one entry")
			assert.Equal(t, got[0].ImportPath, "./missing", "the entry names the pattern")
			assert.NotNil(t, got[0].Error, "the entry states the package's own error")
			assert.HasPrefix(t, got[0].Problem(), "stat ", "the error states the missing directory")
		})

		t.Run("states the errors of a package's dependencies in its entry", func(t *testing.T) {
			t.Parallel()
			dir := moduleDir(t, map[string]string{"a.go": "package fixture\n\nimport _ \"fixture/missing\"\n"})
			got, err := load.List(ctx, load.Config{Dir: dir, Env: os.Environ()}, []string{"."})
			assert.NoError(t, err, "go list lists a package whose dependency does not resolve")
			assert.Length(t, got, 1, "the pattern has one entry")
			assert.Nil(t, got[0].Error, "the package itself lists")
			assert.Contains(t, got[0].Problem(), "package fixture/missing is not in ",
				"the entry states the dependency's error")
		})

		t.Run("asks go list for the fields of Listed alone", func(t *testing.T) {
			t.Parallel()
			env, fake := fakeGo(t)
			fakeList(t, fake, map[string]any{"ImportPath": "fixture", "Dir": "/src/fixture"})
			got, err := load.List(ctx, load.Config{Dir: t.TempDir(), Env: env}, []string{"./a", "./b"})
			assert.NoError(t, err, "the fake go list lists one package")
			assert.Equal(t, got, []load.Listed{{ImportPath: "fixture", Dir: "/src/fixture"}}, "the entry decodes")
			assert.Equal(t, calls(t, fake), "list -e -json=ImportPath,Dir,Error,DepsErrors ./a ./b\n",
				"go list writes the fields that Listed reads, for the patterns in order")
		})

		t.Run("returns an error with the go command's message when go list fails", func(t *testing.T) {
			t.Parallel()
			env, _ := fakeGo(t)
			_, err := load.List(ctx, load.Config{Dir: t.TempDir(), Env: env}, []string{"."})
			assert.HasError(t, err, "a go command without list.json fails")
			assert.That(t, err.Error()).
				HasPrefix("go list -e -json=ImportPath,Dir,Error,DepsErrors .: exit status 1", "the error states the call").
				Contains(listFile, "and the go command's message")
		})

		t.Run("returns an error when an entry does not decode", func(t *testing.T) {
			t.Parallel()
			env, fake := fakeGo(t)
			write(t, fake, listFile, "{")
			_, err := load.List(ctx, load.Config{Dir: t.TempDir(), Env: env}, []string{"."})
			assert.HasError(t, err, "an entry that ends early fails")
			assert.Equal(t, err.Error(), "go list: unexpected EOF", "the error states the decoder's")
		})
	})

	t.Run("Listed", func(t *testing.T) {
		t.Parallel()

		t.Run("Problem", func(t *testing.T) {
			t.Parallel()

			t.Run("returns the package's own error whatever its dependencies state", func(t *testing.T) {
				t.Parallel()
				prop.ForAll(t, "an entry with an error of its own states that error", func(c *prop.Case) {
					l := load.Listed{
						Error:      c.Draw(listError, "own"),
						DepsErrors: c.Draw(prop.List(listError, prop.MaxSize(3)), "deps"),
					}
					assert.Equal(c, l.Problem(), strings.TrimSpace(l.Error.Err),
						"Problem returns the package's own error without the white space around it")
				})
			})

			t.Run("returns the first error of the dependencies of a package without its own", func(t *testing.T) {
				t.Parallel()
				prop.ForAll(
					t,
					"an entry without an error of its own states its first dependency's",
					func(c *prop.Case) {
						l := load.Listed{
							DepsErrors: c.Draw(prop.List(listError, prop.MinSize(1), prop.MaxSize(3)), "deps"),
						}
						assert.Equal(c, l.Problem(), strings.TrimSpace(l.DepsErrors[0].Err),
							"Problem returns the first dependency's error without the white space around it")
					},
				)
			})

			t.Run("returns an empty string for an entry without an error", func(t *testing.T) {
				t.Parallel()
				l := load.Listed{ImportPath: "fixture", Dir: "/src/fixture"}
				assert.Equal(t, l.Problem(), "", "an entry that states no error has no problem")
			})
		})
	})
}
