// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.dokimi.dev/mutate/internal/spec"
)

// The names that decide which files of a directory are a package's data.
const (
	// hiddenPrefix starts the name of a file or a directory that the data
	// leave out, such as .git.
	hiddenPrefix = "."
	// testdata is the directory of a package's test data, which the go
	// command ignores as a package.
	testdata = "testdata"
	// ignoredPrefix starts the name of a directory that the go command
	// ignores as a package.
	ignoredPrefix = "_"
	// goSuffix ends the name of a Go file.
	goSuffix = ".go"
)

// snapshot is the digest of each data file of one package of the suite,
// as Run reads them before any test binary runs.
type snapshot struct {
	// importPath is empty for the run's own package.
	importPath string
	dir        string
	files      map[string]string
}

// readSnapshot returns the SHA-256 digest of each file of the package's
// data in dir, as hexadecimal, by the file's path relative to dir with / as
// the separator.
//
// The package's data are the files of dir, and of the subdirectories of dir
// that contain no package of their own: a directory that contains no Go
// file, or that the go command ignores because its name is testdata or
// starts with _. readSnapshot leaves out every file and directory whose name
// starts with a dot, such as .git. It returns the error of a file or a
// directory that does not read, without a prefix, which the caller adds.
func readSnapshot(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if path != dir && strings.HasPrefix(name, hiddenPrefix) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != dir && name != testdata && !strings.HasPrefix(name, ignoredPrefix) && containsGo(path) {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		sum := sha256.Sum256(content)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// containsGo reports whether dir contains a file whose name ends in .go.
func containsGo(dir string) bool {
	entries, _ := os.ReadDir(dir)
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		return !e.IsDir() && strings.HasSuffix(e.Name(), goSuffix)
	})
}

// changedFiles returns the paths of the files that differ between two
// snapshots of one directory, sorted: each file whose digest changed, that
// after adds, or that after lacks.
func changedFiles(before, after map[string]string) []string {
	var changed []string
	for path, digest := range after {
		if before[path] != digest {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

// compare states the run error changed-files when a data file of a package
// of the suite differs after the runs from the file before them, or was
// added or removed. It compares nothing when no test binary started.
func (r *runner) compare() {
	if !r.ran {
		return
	}
	var changed []string
	for _, s := range r.snapshots {
		after, err := readSnapshot(s.dir)
		if err != nil {
			r.fail(spec.ErrorChangedFiles, "the files do not read after the runs: "+err.Error())
			return
		}
		rel, _ := filepath.Rel(r.rec.Root, s.dir)
		for _, path := range changedFiles(s.files, after) {
			changed = append(changed, filepath.ToSlash(filepath.Join(rel, path)))
		}
	}
	if len(changed) > 0 {
		r.fail(spec.ErrorChangedFiles, "the runs added, changed or removed these files: "+strings.Join(changed, ", "))
	}
}
