// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Snapshot returns the SHA-256 digest of each file of the package's data in
// dir, as hexadecimal, by the file's path relative to dir with / as the
// separator.
//
// The package's data are the files of dir, and of the subdirectories of dir
// that contain no package of their own: a directory that contains no Go
// file, or that the go command ignores because its name is testdata or
// starts with _. Snapshot leaves out every file and directory whose name
// starts with a dot, such as .git.
func Snapshot(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if path != dir && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != dir && name != "testdata" && !strings.HasPrefix(name, "_") && containsGo(path) {
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
		return nil, fmt.Errorf("record: %w", err)
	}
	return files, nil
}

// Files returns the digest of a snapshot, as hexadecimal: of each path and
// its file's digest, in the order of the paths, each behind its length, so
// no two snapshots share a stream.
func Files(snapshot map[string]string) string {
	h := sha256.New()
	field(h, "dokimi-mutate-files/2")
	for _, path := range sorted(snapshot) {
		field(h, path)
		field(h, snapshot[path])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Changed returns the paths of the files that differ between two snapshots
// of one directory, sorted: each file whose digest changed, that after adds,
// or that after lacks.
func Changed(before, after map[string]string) []string {
	var changed []string
	for _, path := range sorted(after) {
		if before[path] != after[path] {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// sorted returns the keys of m in order.
func sorted(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Inputs returns the inputs digest of a run, as sha256:<hex>. Two runs have
// one digest exactly when their engine's identity, their toolchain, the
// build IDs of their instrumented test binaries, and the digests of the
// packages' data that Files returns are equal. buildIDs and files each state
// every test binary or package of the run's suite, in the run's order.
func Inputs(engine, toolchain, buildIDs, files string) string {
	h := sha256.New()
	for _, f := range []string{"dokimi-mutate-inputs/2", engine, toolchain, buildIDs, files} {
		field(h, f)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Identity returns what the inputs digest states of the engine: version,
// which identifies a released engine's code, and buildID, the build ID of
// the engine's executable, after it when version is (devel) or ends in
// +dirty. A build from a working tree reports such a version for different
// code.
func Identity(version, buildID string) string {
	if version == "(devel)" || strings.HasSuffix(version, "+dirty") {
		return version + " " + buildID
	}
	return version
}

// field writes s to h behind its length.
func field(h hash.Hash, s string) {
	h.Write([]byte(strconv.Itoa(len(s)) + ":" + s))
}

// containsGo reports whether dir contains a file whose name ends in .go.
func containsGo(dir string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}
