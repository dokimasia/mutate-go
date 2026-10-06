// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// The module of a fixture: the name of its module file, and the module file
// that fixture writes where a test states none.
const (
	goMod  = "go.mod"
	module = "module fixture\n\ngo 1.21\n"
)

// The modes of the files and the directories that the tests write.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// moduleDir writes files into a new directory, with the module file module
// unless files has one, and returns the directory.
func moduleDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files[goMod]; !ok {
		files[goMod] = module
	}
	for name, text := range files {
		path := filepath.Join(dir, name)
		assert.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode), "the directory of "+name+" is created")
		assert.NoError(t, os.WriteFile(path, []byte(text), fileMode), name+" is written")
	}
	return dir
}

// fixture writes files as moduleDir does, and loads the package in the
// module's root directory.
func fixture(t *testing.T, files map[string]string) *load.Package {
	t.Helper()
	return loadDir(t, moduleDir(t, files))
}

// loadDir loads the package in dir with the include directive of the
// definition, as a run loads it.
func loadDir(t *testing.T, dir string) *load.Package {
	t.Helper()
	d := spec.Load()
	include := d.Overlay.Comment + d.Catalogue.Include
	p, err := load.Load(context.Background(), load.Config{Dir: dir, Env: os.Environ(), Include: include})
	assert.NoError(t, err, "the fixture's package loads")
	return p
}

// enumerateFixture loads files as the package of a fixture module and
// enumerates it under opts.
func enumerateFixture(t *testing.T, files map[string]string, opts enumerate.Options) *enumerate.Result {
	t.Helper()
	return enumerate.Enumerate(fixture(t, files), spec.Load(), opts)
}

// all enumerates every line of the fixture files, without its generated
// files.
func all(t *testing.T, files map[string]string) *enumerate.Result {
	t.Helper()
	return enumerateFixture(t, files, enumerate.Options{})
}

// nth returns each mutant's position among the mutants of its scope and
// kind, from 0 in source order.
func nth(r *enumerate.Result) map[*enumerate.Mutant]int {
	out := map[*enumerate.Mutant]int{}
	seen := map[string]int{}
	for _, m := range r.Mutants {
		id := m.Site.Scope + " " + string(m.Kind)
		out[m] = seen[id]
		seen[id]++
	}
	return out
}

// listing writes one line per mutant whose kind starts with prefix: its
// scope, kind and nth, its source and its replacement, then its rule, its
// reason, and whether it is not viable.
func listing(r *enumerate.Result, prefix string) string {
	var b strings.Builder
	n := nth(r)
	for _, m := range r.Mutants {
		if !strings.HasPrefix(string(m.Kind), prefix) {
			continue
		}
		fmt.Fprintf(&b, "%s %s %d: %s -> %q", m.Site.Scope, m.Kind, n[m], m.Original, m.Replacement)
		if m.Rule != "" {
			fmt.Fprintf(&b, " [%s]", m.Rule)
		}
		if m.Reason != "" && m.Status == enumerate.Suppressed {
			fmt.Fprintf(&b, " (%s)", m.Reason)
		}
		if m.Status == enumerate.NotViable {
			b.WriteString(" not-viable")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// statuses writes one line per mutant that is not runnable: its scope,
// kind and nth, and its status as the verdict that it gives.
func statuses(r *enumerate.Result) string {
	verdicts := map[enumerate.Status]spec.Verdict{
		enumerate.Suppressed:  spec.Suppressed,
		enumerate.NotViable:   spec.NotViable,
		enumerate.NotSelected: spec.NotSelected,
	}
	var b strings.Builder
	n := nth(r)
	for _, m := range r.Mutants {
		if m.Status != enumerate.Runnable {
			fmt.Fprintf(&b, "%s %s %d: %s\n", m.Site.Scope, m.Kind, n[m], verdicts[m.Status])
		}
	}
	return b.String()
}

// skipped writes one line per skipped site of the file src: its source
// and its reason.
func skipped(r *enumerate.Result, src string) string {
	var b strings.Builder
	for _, s := range r.Skipped {
		fmt.Fprintf(&b, "%s: %s\n", src[offset(src, s.Start):offset(src, s.End)], s.Reason)
	}
	return b.String()
}

// offset returns the byte offset of p in src.
func offset(src string, p enumerate.Position) int {
	line := 1
	for i := range len(src) {
		if line == p.Line {
			return i + p.Column - 1
		}
		if src[i] == '\n' {
			line++
		}
	}
	return len(src)
}

// problems writes one line per problem: its code and its message.
func problems(r *enumerate.Result) string {
	var b strings.Builder
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "%s %s\n", p.Code, p.Message)
	}
	return b.String()
}
