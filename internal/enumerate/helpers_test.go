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

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
)

// fixture writes files into a new directory, with a go.mod of the module
// fixture at go 1.21 unless files has one, and loads the package there.
func fixture(t *testing.T, files map[string]string) *load.Package {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module fixture\n\ngo 1.21\n"
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return loadDir(t, dir)
}

// loadDir loads the package in dir with the include directive of the
// definition, as a run loads it.
func loadDir(t *testing.T, dir string) *load.Package {
	t.Helper()
	d := spec.Load()
	include := d.Overlay.Comment + d.Catalogue.Include
	p, err := load.Load(context.Background(), load.Config{Dir: dir, Env: os.Environ(), Include: include})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// enumerateFixture loads files as the package of a fixture module and
// enumerates it with lines as the selection.
func enumerateFixture(t *testing.T, files map[string]string, lines ...enumerate.Range) *enumerate.Result {
	t.Helper()
	return enumerate.Enumerate(fixture(t, files), spec.Load(), lines)
}

// nth returns each mutant's position among the mutants of its scope and
// kind, from 0 in source order.
func nth(r *enumerate.Result) map[*enumerate.Mutant]int {
	out := map[*enumerate.Mutant]int{}
	seen := map[string]int{}
	for _, m := range r.Mutants {
		id := m.Site.Scope + " " + m.Kind
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
		if !strings.HasPrefix(m.Kind, prefix) {
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
// kind and nth, and its status.
func statuses(r *enumerate.Result) string {
	names := map[enumerate.Status]string{
		enumerate.Suppressed:  "suppressed",
		enumerate.NotViable:   "not-viable",
		enumerate.NotSelected: "not-selected",
	}
	var b strings.Builder
	n := nth(r)
	for _, m := range r.Mutants {
		if m.Status != enumerate.Runnable {
			fmt.Fprintf(&b, "%s %s %d: %s\n", m.Site.Scope, m.Kind, n[m], names[m.Status])
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
	for i := 0; i < len(src); i++ {
		if line == p.Line {
			return i + p.Column - 1
		}
		if src[i] == '\n' {
			line++
		}
	}
	return len(src)
}

// want fails t when got differs from want.
func want(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
