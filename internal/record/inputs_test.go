// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/record"
)

// tree writes files, by slash-separated path, into a new directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// digest returns the inputs digest of files with fixed engine, toolchain
// and build fields.
func digest(t *testing.T, files map[string]string) string {
	t.Helper()
	return record.Inputs("v0.1.0", "go1.27.1", "a/b", record.Files(snapshot(t, tree(t, files))))
}

// snapshot returns the snapshot of dir.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := record.Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestInputs(t *testing.T) {
	t.Parallel()
	base := map[string]string{
		"a.go":            "package a",
		"testdata/in.txt": "in",
		"_data/x.go":      "package x",
		"assets/logo.svg": "<svg/>",
	}
	with := func(name, text string) map[string]string {
		files := map[string]string{}
		for k, v := range base {
			files[k] = v
		}
		if text == "" {
			delete(files, name)
		} else {
			files[name] = text
		}
		return files
	}
	t.Run("Inputs", func(t *testing.T) {
		t.Parallel()
		t.Run("returns a SHA-256 digest that equal inputs share", func(t *testing.T) {
			t.Parallel()
			d := digest(t, base)
			if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(d) || d != digest(t, base) {
				t.Errorf("Inputs() = %s, want one sha256 digest for equal inputs", d)
			}
		})
		t.Run("changes with each field and each data file", func(t *testing.T) {
			t.Parallel()
			files := record.Files(snapshot(t, tree(t, base)))
			seen := map[string]string{}
			for name, fields := range map[string][3]string{
				"engine":    {"v0.2.0", "go1.27.1", "a/b"},
				"toolchain": {"v0.1.0", "go1.27.2", "a/b"},
				"build":     {"v0.1.0", "go1.27.1", "a/c"},
				"fields":    {"v0.1.0", "go1.27.1", "a/b"},
			} {
				seen[record.Inputs(fields[0], fields[1], fields[2], files)] = name
			}
			for name, files := range map[string]map[string]string{
				"source":      with("a.go", "package b"),
				"testdata":    with("testdata/in.txt", "out"),
				"underscore":  with("_data/x.go", "package y"),
				"subdir":      with("assets/logo.svg", "<svg></svg>"),
				"new file":    with("notes.txt", "x"),
				"no testdata": with("testdata/in.txt", ""),
			} {
				seen[digest(t, files)] = name
			}
			if len(seen) != 10 {
				t.Errorf("Inputs() gave %d digests for 10 different inputs: %v", len(seen), seen)
			}
		})
	})
	t.Run("Snapshot", func(t *testing.T) {
		t.Parallel()
		t.Run("states the SHA-256 digest of each data file by its path", func(t *testing.T) {
			t.Parallel()
			got := snapshot(t, tree(t, map[string]string{"a.go": "package a", "testdata/in.txt": "in"}))
			// Each digest was computed with printf and sha256sum, outside Go.
			want := map[string]string{
				"a.go":            "7663fa2eaf2e6846391a250cc37947941ffda1650e53cdee850c32f56e277971",
				"testdata/in.txt": "582967534d0f909d196b97f9e6921342777aea87b46fa52df165389db1fb8ccf",
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("Snapshot() = %v, want %v", got, want)
			}
		})
		t.Run("ignores hidden files and the directories of other packages", func(t *testing.T) {
			t.Parallel()
			d := digest(t, base)
			for name, files := range map[string]map[string]string{
				"hidden file":      with(".notes", "x"),
				"hidden directory": with(".git/HEAD", "ref"),
				"other package":    with("sub/s.go", "package sub"),
				"package data":     with("sub/data.txt", "x"),
			} {
				if name == "package data" {
					files["sub/s.go"] = "package sub"
				}
				if got := digest(t, files); got != d {
					t.Errorf("%s changes the digest", name)
				}
			}
		})
		t.Run("returns an error when a file does not read", func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"a.go": "package a"})
			if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "dangling")); err != nil {
				t.Fatal(err)
			}
			if _, err := record.Snapshot(dir); err == nil || !strings.HasPrefix(err.Error(), "record: ") {
				t.Errorf("Snapshot() error = %v", err)
			}
		})
		t.Run("returns an error when the directory does not exist", func(t *testing.T) {
			t.Parallel()
			if _, err := record.Snapshot(filepath.Join(t.TempDir(), "missing")); err == nil {
				t.Error("Snapshot() returned no error")
			}
		})
	})
	t.Run("Changed", func(t *testing.T) {
		t.Parallel()
		t.Run("returns each changed, added and removed path in order", func(t *testing.T) {
			t.Parallel()
			before := map[string]string{"a": "1", "b": "2", "c": "3", "e": "5"}
			after := map[string]string{"a": "1", "b": "9", "d": "4", "e": "5"}
			if got := strings.Join(record.Changed(before, after), " "); got != "b c d" {
				t.Errorf("Changed() = %q, want b c d", got)
			}
			if got := record.Changed(before, before); len(got) != 0 {
				t.Errorf("Changed() of equal snapshots = %q, want none", got)
			}
		})
	})
	t.Run("Identity", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name, give, want string
		}{
			{"returns a released version alone", "v0.1.0", "v0.1.0"},
			{"adds the build ID to a development build", "(devel)", "(devel) x/y"},
			{
				"adds the build ID to a build from a modified working tree",
				"v0.1.1-0.20261005120000-0123456789ab+dirty",
				"v0.1.1-0.20261005120000-0123456789ab+dirty x/y",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := record.Identity(tt.give, "x/y"); got != tt.want {
					t.Errorf("Identity(%q) = %q, want %q", tt.give, got, tt.want)
				}
			})
		}
	})
}
