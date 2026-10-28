// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// The names that decide what a file of an observation is to its package: the
// suffix of a test file, the separator of a directory in a file's path, and
// the path of the root's own directory.
const (
	testSuffix   = "_test.go"
	dirSeparator = "/"
	rootDir      = "."
)

// settingsSeparator separates the settings of a run in the requirements
// digest, and suiteSeparator the import paths of its suite.
const (
	settingsSeparator = "\n"
	suiteSeparator    = ","
)

// origin is what a package of an observation is to the run: the run's own
// package, another package of the suite, or a package of a main module that
// the package imports.
type origin uint8

const (
	ownOrigin origin = iota
	suiteOrigin
	importedOrigin
)

// region is the directory of one package of the observation, relative to
// the root with / as the separator and "." for the root, and the package's
// origin.
type region struct {
	dir    string
	origin origin
}

// observe sets the run's observation and the regions of its packages, after
// the region of the root. The
// observation states the digest of each file of the run's snapshots and of
// the packages that the package imports, by the file's path relative to the
// root, and the requirements digest. The requirements digest covers the
// engine's identity, the toolchain, the catalogue's and the overlay's
// versions, the run's suite, Confirm and IncludeGenerated, and the content
// of each requirement file of the package. A requirement file that does not
// exist or does not read is left out, so its absence is part of the digest.
func (r *runner) observe() {
	files := map[string]string{}
	r.regions = []region{{dir: rootDir, origin: ownOrigin}}
	for k, s := range slices.Concat(r.snapshots, r.imported) {
		dir := r.relative(s.dir)
		o := importedOrigin
		switch {
		case k == 0:
			o = ownOrigin
		case k < len(r.snapshots):
			o = suiteOrigin
		}
		r.regions = append(r.regions, region{dir: dir, origin: o})
		for name, content := range s.files {
			files[path.Join(dir, name)] = observed(content)
		}
	}
	requirements := map[string]string{}
	if r.pkg != nil {
		for _, file := range r.pkg.Requirements {
			info, err := os.Lstat(file)
			var content string
			if err == nil {
				content, err = fileDigest(file, info.Mode().Type())
			}
			if err == nil {
				requirements[r.relative(file)] = content
			}
		}
	}
	settings := strings.Join([]string{
		strings.Join(r.rec.Suite, suiteSeparator),
		strconv.FormatBool(r.cfg.Confirm),
		strconv.FormatBool(r.cfg.IncludeGenerated),
	}, settingsSeparator)
	r.observation = &record.Observation{
		StartedAt: r.rec.StartedAt,
		Files:     files,
		Requirements: record.Requirements(
			r.engine, r.rec.Toolchain, r.rec.Catalogue, r.rec.Overlay, settings, record.Files(requirements),
		),
	}
}

// observed returns the digest of a file of an observation from what a
// snapshot states of the file: the SHA-256 digest of a regular file's
// content, and the SHA-256 digest of what the snapshot states of a link or
// of another file that is not regular.
func observed(content string) string {
	if strings.HasPrefix(content, linkPrefix) || strings.HasPrefix(content, specialPrefix) {
		sum := sha256.Sum256([]byte(content))
		content = hex.EncodeToString(sum[:])
	}
	return record.DigestPrefix + content
}

// relative returns the path of the file or the directory p relative to the
// record's root, with / as the separator.
func (r *runner) relative(p string) string {
	// The root and p are absolute paths with their links resolved, which
	// filepath.Rel relates on every system.
	rel, _ := filepath.Rel(r.rec.Root, p)
	return filepath.ToSlash(rel)
}

// declare sets the file that declares each test of each program, by the
// program's target and the test's top-level name, as a path relative to the
// root. It returns the error of a test file that does not read or parse.
func (r *runner) declare() error {
	r.declared = map[string]map[string]string{}
	for _, p := range r.programs {
		names, err := load.Declarations(p.dir, p.testFiles)
		if err != nil {
			return err
		}
		files := map[string]string{}
		for test, name := range names {
			files[test] = r.relative(filepath.Join(p.dir, name))
		}
		r.declared[p.target] = files
	}
	return nil
}

// groups returns the order of the runs of pending, the record's indexes of
// the mutants without a verdict in the order of their keys, in a run with a
// history, and the earlier verdict of each mutant that the run may reuse,
// by index. The order starts with the mutants without a reusable verdict in
// the order of pending. Then come the mutants whose reusable verdict's wider
// inputs changed since the run that observed it, and then the other mutants
// with a reusable verdict. Each of the two later groups runs the oldest
// observation first, and then in the order of pending.
func (r *runner) groups(pending []int) (order []int, reused map[int]*record.Mutant) {
	r.observe()
	reused = map[int]*record.Mutant{}
	if r.cfg.Earlier == nil {
		return pending, reused
	}
	observations := map[string]*record.Observation{}
	for i := range r.cfg.Earlier.Observations {
		o := &r.cfg.Earlier.Observations[i]
		observations[o.StartedAt] = o
	}
	// earlier lists the mutants of the earlier record by key, in record
	// order. The n-th mutant of a key in pending gets the n-th of the key.
	earlier := map[string][]*record.Mutant{}
	for i := range r.cfg.Earlier.Mutants {
		e := &r.cfg.Earlier.Mutants[i]
		earlier[e.Key] = append(earlier[e.Key], e)
	}
	var fresh, wider, rest []int
	for _, i := range pending {
		m := &r.rec.Mutants[i]
		queue := earlier[m.Key]
		if len(queue) == 0 {
			fresh = append(fresh, i)
			continue
		}
		e := queue[0]
		earlier[m.Key] = queue[1:]
		o := observations[e.ObservedAt]
		switch {
		case o == nil || !r.reusable(m, e, o):
			fresh = append(fresh, i)
		case r.changed(o, func(from origin, _ string) bool { return from != suiteOrigin }):
			reused[i] = e
			wider = append(wider, i)
		default:
			reused[i] = e
			rest = append(rest, i)
		}
	}
	oldest := func(a, b int) int { return strings.Compare(reused[a].ObservedAt, reused[b].ObservedAt) }
	slices.SortStableFunc(wider, oldest)
	slices.SortStableFunc(rest, oldest)
	return slices.Concat(fresh, wider, rest), reused
}

// reusable reports whether the run may reuse e, the earlier verdict of the
// mutant m, which the observation o observed. o must have the run's
// requirements digest, and m's file must be unchanged since o. A kill, a
// time-out and an exhaustion also need each file that declares a test of
// e's tests to be unchanged. A survivor also needs every test file and
// every data file of the suite to be unchanged, none of them added or
// removed. No other verdict is reusable.
func (r *runner) reusable(m, e *record.Mutant, o *record.Observation) bool {
	if o.Requirements != r.observation.Requirements || !r.unchanged(o, m.File) {
		return false
	}
	switch e.Verdict {
	case spec.Killed, spec.TimedOut, spec.Exhausted:
		for _, test := range e.Tests {
			target, name, ok := strings.Cut(test, targetSeparator)
			if !ok {
				target, name = "", test
			}
			name, _, _ = strings.Cut(name, dirSeparator)
			file, ok := r.declared[target][name]
			if !ok || !r.unchanged(o, file) {
				return false
			}
		}
		return true
	case spec.Survived:
		return !r.changed(o, func(from origin, name string) bool {
			code := !strings.Contains(name, dirSeparator) && strings.HasSuffix(name, goSuffix) &&
				!strings.HasSuffix(name, testSuffix)
			return from != importedOrigin && !code
		})
	case spec.NoCoverage, spec.NotViable, spec.Suppressed, spec.NotSelected, spec.NotRun, spec.Error:
		// The run decides the first four again without a mutant run, and the
		// last two are no verdict of the mutant's tests.
	}
	return false
}

// unchanged reports whether file, a path relative to the root, has the same
// digest in the run's observation as in the observation o. A file that the
// run's observation lacks is not unchanged.
func (r *runner) unchanged(o *record.Observation, file string) bool {
	digest, ok := r.observation.Files[file]
	return ok && o.Files[file] == digest
}

// changed reports whether a file that counts was added, changed or removed
// between the observation o and the run's observation. counts receives the
// origin of the file's package and the file's path in the package's
// directory. A file belongs to the package of the longest directory that
// contains it, and a file outside every package's directory belongs to the
// run's own package, with its path relative to the root.
func (r *runner) changed(o *record.Observation, counts func(o origin, name string) bool) bool {
	for _, files := range []map[string]string{o.Files, r.observation.Files} {
		for file := range files {
			if o.Files[file] == r.observation.Files[file] {
				continue
			}
			// The first region is the root, so every file has a region, and
			// a later region of the same directory takes its place.
			var from origin
			var name string
			longest := -1
			for _, g := range r.regions {
				prefix := g.dir + dirSeparator
				if g.dir == rootDir {
					prefix = ""
				}
				if rest, ok := strings.CutPrefix(file, prefix); ok && len(prefix) >= longest {
					from, name, longest = g.origin, rest, len(prefix)
				}
			}
			if counts(from, name) {
				return true
			}
		}
	}
	return false
}

// reuse gives the mutant at index i the earlier verdict e: its verdict, its
// tests, its covering tests, its time, its confirmation and the start of
// the run that observed it. It then reports the mutant.
func (r *runner) reuse(i int, e *record.Mutant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := &r.rec.Mutants[i]
	m.Verdict, m.Tests, m.CoveredBy, m.Seconds, m.Confirmed, m.ObservedAt =
		e.Verdict, e.Tests, e.CoveredBy, e.Seconds, e.Confirmed, e.ObservedAt
	r.report(i)
}

// observations returns the record's observations: the run's own, and then
// each observation of the earlier record that a verdict of the record
// names, in the earlier record's order.
func (r *runner) observations() []record.Observation {
	if r.observation == nil {
		r.observe()
	}
	list := []record.Observation{*r.observation}
	if r.cfg.Earlier == nil {
		return list
	}
	for _, o := range r.cfg.Earlier.Observations {
		named := slices.ContainsFunc(r.rec.Mutants, func(m record.Mutant) bool { return m.ObservedAt == o.StartedAt })
		if named && o.StartedAt != r.rec.StartedAt {
			list = append(list, o)
		}
	}
	return list
}
