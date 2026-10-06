// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/render"
	"go.dokimi.dev/mutate/internal/spec"
)

// helperFile is the name of the helper file in a package directory that
// does not use it.
const helperFile = "zz_mutate.go"

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("Render", func(t *testing.T) {
		t.Parallel()

		t.Run("computes each mutant as the catalogue defines it when its ordinal is active", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, semanticsFiles())
			prog, bin := build(t, p, r)
			assert.Equal(t, describe(lines(run(t, bin, p.Dir, active(0)))), describe(semanticsOriginal),
				"with no mutant active the package computes as its source")
			names := ids(r)
			var ran []string
			for _, m := range r.Mutants {
				if m.Status != enumerate.Runnable {
					continue
				}
				got := lines(run(t, bin, p.Dir, active(prog.Ordinals[m])))
				expect.Equal(t, describe(got), describe(expected(names, m)),
					names[m]+" computes what the catalogue states while its ordinal is active")
				ran = append(ran, names[m])
			}
			assert.Permutation(t, ran, slices.Collect(maps.Keys(semanticsWant)),
				"every mutant of the fixture runs, and no other")
		})

		t.Run("numbers the mutants of the instrumented sites consecutively in source order", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go": "package fixture\n\nfunc f(a, b int) bool {\n\treturn a < b //dokimi:mutate-skip ror-false: a test of the numbering\n}\n\n" +
					"func g(a int) int {\n\t//dokimi:mutate-skip sbr-zero: a test of the numbering\n\treturn a * 2\n}\n",
			})
			prog := instrument(t, p, r)
			var got []string
			for _, m := range r.Mutants {
				got = append(got, string(m.Kind)+" "+strconv.Itoa(prog.Ordinals[m]))
			}
			assert.Equal(t, got, []string{"sbr-zero 1", "ror-boundary 2", "ror-false 3", "sbr-zero 0", "aor 4"},
				"the suppressed mutants have no ordinal")
			assert.Length(t, prog.Sites, 3, "Sites lists the three sites with a runnable mutant")
		})

		t.Run("names the helper file after the names that the package directory uses", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":     "package fixture\n\nfunc F(x int) int { return x + 1 }\n",
				helperFile: "//go:build ignore\n\npackage fixture\n",
			})
			prog := instrument(t, p, r)
			assert.Contains(t, prog.Files, filepath.Join(p.Dir, "zz_mutate_1.go"),
				"the helper file takes the first free name")
		})

		t.Run("marks the mutants of a site whose form the type checker rejects not viable", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go": "package fixture\n\nfunc F(a, b int) bool { return a < b }\n\nfunc G(x int) int { return x + 1 }\n",
			})
			for _, s := range r.Sites {
				if s.Form == enumerate.Ordered {
					s.TypeArg = "string"
				}
			}
			prog := instrument(t, p, r)
			for _, m := range r.Mutants {
				if m.Site.Form == enumerate.Ordered {
					expect.Equal(t, m.Status, enumerate.NotViable, string(m.Kind)+" of the rejected form is not viable")
					expect.NotEqual(t, m.Reason, "", "and states the type checker's message")
					expect.Equal(t, prog.Ordinals[m], 0, "and has no ordinal")
				} else {
					expect.Equal(t, m.Status, enumerate.Runnable, string(m.Kind)+" of another site runs")
				}
			}
		})

		t.Run("marks the mutants of a site whose form does not parse not viable", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			for _, s := range r.Sites {
				if s.Form == enumerate.Zero {
					s.Zeros = []string{"}"}
				}
			}
			instrument(t, p, r)
			for _, m := range r.Mutants {
				expect.Equal(t, m.Status == enumerate.NotViable, m.Kind == spec.SBRZero,
					"only the zero return, whose form does not parse, is not viable: "+string(m.Kind))
			}
		})

		t.Run("gives up a site on its primary error and ignores the secondary lines", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			for _, s := range r.Sites {
				if s.Form == enumerate.Zero {
					s.Zeros = []string{"func() int { type x int; type x int; return 0 }()"}
				}
			}
			instrument(t, p, r)
			for _, m := range r.Mutants {
				if m.Kind == spec.SBRZero {
					expect.Equal(t, m.Status, enumerate.NotViable, "the zero return is not viable")
					expect.Contains(t, m.Reason, "redeclared", "for the redeclaration")
				} else {
					expect.Equal(t, m.Status, enumerate.Runnable, string(m.Kind)+" runs")
				}
			}
		})

		t.Run("returns an error when the type checker rejects the package outside every form", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			p.Name = "other"
			_, err := render.Render(p, r, mutantVar)
			assert.HasError(t, err, "a helper file of another package does not type-check")
			assert.HasPrefix(t, err.Error(), "render: the type checker rejects the instrumented package: ",
				"the error names the rejection")
		})

		t.Run("returns an error when the package directory does not list", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{"f.go": "package fixture\n\nfunc F(a int) int { return a + 1 }\n"})
			p.Dir = filepath.Join(p.Dir, "f.go")
			_, err := render.Render(p, r, mutantVar)
			assert.HasError(t, err, "a file in place of the directory fails")
			assert.HasPrefix(t, err.Error(), "render: ", "the error starts with the package's name")
		})
	})

	t.Run("Program", func(t *testing.T) {
		t.Parallel()

		t.Run("Write", func(t *testing.T) {
			t.Parallel()
			prog := &render.Program{Files: map[string][]byte{"/pkg/b.go": []byte("b"), "/pkg/a.go": []byte("a")}}

			t.Run("writes every file and an overlay that maps each path to its copy", func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				overlay, err := prog.Write(dir)
				assert.NoError(t, err, "the program writes")
				data, err := os.ReadFile(overlay)
				assert.NoError(t, err, "the overlay reads")
				var got struct{ Replace map[string]string }
				assert.NoError(t, json.Unmarshal(data, &got), "the overlay is the go command's JSON")
				assert.Equal(t, got.Replace, map[string]string{
					"/pkg/a.go": filepath.Join(dir, "0", "a.go"),
					"/pkg/b.go": filepath.Join(dir, "1", "b.go"),
				}, "each file has a copy in a directory of its own, in the order of the paths")
				copied, err := os.ReadFile(filepath.Join(dir, "0", "a.go"))
				assert.NoError(t, err, "the copy reads")
				assert.Equal(t, string(copied), "a", "and has the file's text")
			})

			tests := []struct {
				name    string
				blocked string
				file    bool
			}{
				{name: "returns an error when a file's directory cannot be made", blocked: "0", file: true},
				{name: "returns an error when a file does not write", blocked: filepath.Join("0", "a.go")},
				{name: "returns an error when the overlay does not write", blocked: "overlay.json"},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					dir := t.TempDir()
					blocked := filepath.Join(dir, tt.blocked)
					assert.NoError(
						t,
						os.MkdirAll(filepath.Dir(blocked), dirMode),
						"the blocked path's directory exists",
					)
					if tt.file {
						assert.NoError(t, os.WriteFile(blocked, nil, fileMode), "a file blocks the path")
					} else {
						assert.NoError(t, os.Mkdir(blocked, dirMode), "a directory blocks the path")
					}
					_, err := prog.Write(dir)
					assert.HasError(t, err, "Write fails")
					assert.HasPrefix(t, err.Error(), "render: ", "the error starts with the package's name")
				})
			}
		})
	})
}
