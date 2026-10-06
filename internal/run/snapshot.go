// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.dokimi.dev/mutate/internal/spec"
)

// The names that decide which files of a directory are a package's data.
const (
	// hiddenPrefix starts the name of a directory that a tool keeps its
	// state in, such as .git, which the data leave out.
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

// The prefixes of what a snapshot states of a file that is no regular file:
// the target of a symbolic link, and the type of any other file, such as a
// named pipe. Neither is the hexadecimal digest of a regular file.
const (
	linkPrefix    = "link "
	specialPrefix = "special "
)

// snapshot is the digest of each data file of one package of the suite,
// as Run reads them before any test binary runs.
type snapshot struct {
	// importPath is empty for the run's own package.
	importPath string
	dir        string
	files      map[string]string
}

// readSnapshot returns what it reads of each file of the package's data in
// dir, by the file's path relative to dir with / as the separator: the
// SHA-256 digest of a regular file's content as hexadecimal, the target of a
// symbolic link after linkPrefix, and the type of any other file after
// specialPrefix. It opens regular files alone, reads each as a stream, and
// follows no link, so a named pipe or a link to a device does not block it.
//
// The package's data are the files of dir, and of the subdirectories of dir
// that contain no package of their own: a directory that contains no Go
// file, and every directory inside a directory that the go command ignores,
// whose name is testdata or starts with _. Outside such an ignored
// directory, readSnapshot leaves out each directory whose name starts with
// a dot, such as .git, where a tool keeps its state. It returns the error of
// a file or a directory that does not read, without a prefix, which the
// caller adds.
func readSnapshot(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// An ignored directory, and every directory inside one, is data,
			// whatever its name and its content.
			ignored := slices.ContainsFunc(strings.Split(rel, "/"), func(part string) bool {
				return part == testdata || strings.HasPrefix(part, ignoredPrefix)
			})
			if !ignored && (strings.HasPrefix(d.Name(), hiddenPrefix) || containsGo(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		digest, err := fileDigest(path, d.Type())
		if err != nil {
			return err
		}
		files[rel] = digest
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// fileDigest returns what a snapshot states of the file at path, whose type
// is typ, as readSnapshot states it. It returns the error of a regular file
// that does not open or read, or of a link that does not read, beside a
// digest that the caller discards.
func fileDigest(path string, typ fs.FileMode) (string, error) {
	switch {
	case typ.IsRegular():
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		h := sha256.New()
		_, err = io.Copy(h, f)
		return hex.EncodeToString(h.Sum(nil)), err
	case typ&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		return linkPrefix + target, err
	}
	return specialPrefix + typ.String(), nil
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
