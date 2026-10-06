// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/load"
)

// testFile returns a test file of the package pkg that declares one test,
// with spec as its import beside testing.
func testFile(pkg, spec string) string {
	return "package " + pkg + "\n\nimport (\n\t\"testing\"\n\n\t" + spec + "\n)\n\nfunc TestX(t *testing.T) {}\n"
}

func TestLinking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("Linking", func(t *testing.T) {
		t.Parallel()

		t.Run("lists the packages whose test binaries link the package by import path", func(t *testing.T) {
			t.Parallel()
			dir := resolved(t, moduleDir(t, map[string]string{
				// fixture imports fixture/b, so the tests of b recompile
				// fixture for b's test binary.
				"f.go":        "package fixture\n\nimport \"fixture/b\"\n\nvar V = b.V\n",
				"f_test.go":   testFile("fixture", "_ \"fixture/a\""),
				"a/a.go":      "package a\n",
				"a/a_test.go": testFile("a_test", "_ \"fixture\""),
				"b/b.go":      "package b\n\nvar V = 1\n",
				"b/b_test.go": testFile("b_test", "_ \"fixture\""),
				"c/c.go":      "package c\n\nimport _ \"fixture\"\n",
				"d/d.go":      "package d\n",
				"d/d_test.go": testFile("d", "_ \"strings\""),
			}))
			linked, err := load.Linking(ctx, load.Config{Dir: dir, Env: os.Environ()}, "fixture", []string{"./..."})
			assert.NoError(t, err, "the module's packages list")
			assert.Equal(t, linked, []load.Linked{
				{ImportPath: "fixture/a", Dir: filepath.Join(dir, "a")},
				{ImportPath: "fixture/b", Dir: filepath.Join(dir, "b")},
			}, "a and b link fixture in their test binaries, c has no test binary, and d does not link fixture")
		})

		t.Run("lists no package when no test binary links the package", func(t *testing.T) {
			t.Parallel()
			dir := moduleDir(t, map[string]string{"a.go": "package fixture\n", "x/x.go": "package x\n"})
			linked, err := load.Linking(ctx, load.Config{Dir: dir, Env: os.Environ()}, "fixture", []string{"./x"})
			assert.NoError(t, err, "the package lists")
			assert.Empty(t, linked, "x has no test binary")
		})

		tests := []struct {
			name string
			give func(t *testing.T) load.Config
			// prefix starts the error, and text is in it.
			prefix, text string
		}{
			{
				name: "returns an error when PATH names no go command",
				give: func(t *testing.T) load.Config {
					t.Helper()
					return load.Config{Dir: t.TempDir(), Env: withPath(os.Environ(), t.TempDir())}
				},
				prefix: "no go command in the directories of PATH",
			},
			{
				name: "returns an error when an entry does not decode",
				give: func(t *testing.T) load.Config {
					t.Helper()
					env, fake := fakeGo(t)
					write(t, fake, listFile, "{")
					return load.Config{Dir: t.TempDir(), Env: env}
				},
				prefix: "go list: unexpected EOF",
			},
			{
				name: "returns the error that go list states of a package after its import path",
				give: func(t *testing.T) load.Config {
					t.Helper()
					return load.Config{Dir: moduleDir(t, map[string]string{"a.go": "pkg fixture\n"}), Env: os.Environ()}
				},
				prefix: "fixture: ",
				text:   "expected 'package'",
			},
			{
				name: "returns the error of a dependency after the import path of the package",
				give: func(t *testing.T) load.Config {
					t.Helper()
					return load.Config{
						Dir: moduleDir(
							t,
							map[string]string{"a.go": "package fixture\n\nimport _ \"fixture/missing\"\n"},
						),
						Env: os.Environ(),
					}
				},
				prefix: "fixture: package fixture/missing is not in ",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				_, err := load.Linking(ctx, tt.give(t), "fixture", []string{"./..."})
				assert.HasError(t, err, "Linking fails")
				assert.That(t, err.Error()).
					HasPrefix(tt.prefix, "the error starts with the package or the call").
					Contains(tt.text, "and states the cause")
			})
		}
	})
}
