// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/load"
)

func TestLinking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("Linking", func(t *testing.T) {
		t.Parallel()
		t.Run("lists the packages whose test binaries link the package, by import path", func(t *testing.T) {
			t.Parallel()
			test := func(pkg, imports string) string {
				return "package " + pkg + "\n\nimport (\n\t\"testing\"\n\n\t" + imports + "\n)\n\nfunc TestX(t *testing.T) {}\n"
			}
			dir := module(t, map[string]string{
				// fixture imports fixture/b, so the tests of b recompile
				// fixture for b's test binary.
				"f.go":        "package fixture\n\nimport \"fixture/b\"\n\nvar V = b.V\n",
				"f_test.go":   test("fixture", "_ \"fixture/a\""),
				"a/a.go":      "package a\n",
				"a/a_test.go": test("a_test", "_ \"fixture\""),
				"b/b.go":      "package b\n\nvar V = 1\n",
				"b/b_test.go": test("b_test", "_ \"fixture\""),
				"c/c.go":      "package c\n\nimport _ \"fixture\"\n",
				"d/d.go":      "package d\n",
				"d/d_test.go": test("d", "_ \"strings\""),
			})
			linked, err := load.Linking(ctx, load.Config{Dir: dir, Env: os.Environ()}, "fixture", []string{"./..."})
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprint([]load.Linked{
				{ImportPath: "fixture/a", Dir: filepath.Join(dir, "a")},
				{ImportPath: "fixture/b", Dir: filepath.Join(dir, "b")},
			})
			if got := fmt.Sprint(linked); got != want {
				t.Errorf("Linking() = %s, want %s", got, want)
			}
		})
	})
	t.Run("Linking errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			give func(t *testing.T) load.Config
			want string
		}{
			{"returns an error when PATH names no go command", func(t *testing.T) load.Config {
				t.Helper()
				return load.Config{Dir: t.TempDir(), Env: withPath(os.Environ(), t.TempDir())}
			}, "no go command in the directories of PATH"},
			{"returns an error when go list prints no JSON", func(t *testing.T) load.Config {
				t.Helper()
				env, fake := fakeGo(t)
				write(t, fake, "list.json", "{")
				return load.Config{Dir: t.TempDir(), Env: env}
			}, "go list: unexpected EOF"},
			{"returns the error that go list states of a package", func(t *testing.T) load.Config {
				t.Helper()
				return load.Config{Dir: module(t, map[string]string{"a.go": "pkg fixture\n"}), Env: os.Environ()}
			}, "expected 'package'"},
			{"returns the error of a dependency after the import path of the package", func(t *testing.T) load.Config {
				t.Helper()
				return load.Config{
					Dir: module(t, map[string]string{"a.go": "package fixture\n\nimport _ \"fixture/missing\"\n"}),
					Env: os.Environ(),
				}
			}, "fixture: package fixture/missing is not in "},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				_, err := load.Linking(ctx, tt.give(t), "fixture", []string{"./..."})
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Errorf("Linking() error = %v, want one that contains %q", err, tt.want)
				}
			})
		}
	})
}
