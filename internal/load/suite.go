// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// The names that go list -test gives the packages of a test binary.
const (
	// testBinary ends the import path of the main package of a test
	// binary: go list -test names the binary of the package P P.test.
	testBinary = ".test"
	// variant starts the suffix of a package that a test binary
	// recompiles, such as the package P in P [Q.test].
	variant = " ["
)

// Linked is a package whose test binary links another package.
type Linked struct {
	ImportPath string
	// Dir is the package directory, where its test binary runs.
	Dir string
}

// listedTest is the part of an entry of go list -test that Linking reads.
type listedTest struct {
	Listed
	Deps []string
}

// Linking lists the packages that patterns name, as go list resolves them
// in cfg.Dir, whose test binaries link the package importPath, in the order
// of their import paths. The package importPath itself is not listed, and
// neither is a package without a test file, which has no test binary.
//
// # Errors
//
// Linking returns an error when the go command fails, and when go list
// states an error of a package that patterns name or of one of its
// dependencies. The error starts with the package's import path, and states
// the first error that go list states of it, as Listed.Problem returns it.
func Linking(ctx context.Context, cfg Config, importPath string, patterns []string) ([]Linked, error) {
	entries, err := list[listedTest](ctx, cfg.Dir, cfg.Env, append([]string{listTests}, patterns...)...)
	if err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	var mains []listedTest
	for _, e := range entries {
		if problem := e.Problem(); problem != "" {
			return nil, fmt.Errorf("%s: %s", e.ImportPath, problem)
		}
		if strings.HasSuffix(e.ImportPath, testBinary) {
			mains = append(mains, e)
		} else {
			listed[e.ImportPath] = true
		}
	}
	slices.SortFunc(mains, func(a, b listedTest) int { return cmp.Compare(a.ImportPath, b.ImportPath) })
	var linked []Linked
	for _, m := range mains {
		p := strings.TrimSuffix(m.ImportPath, testBinary)
		if p != importPath && listed[p] && links(m.Deps, importPath) {
			linked = append(linked, Linked{ImportPath: p, Dir: m.Dir})
		}
	}
	return linked, nil
}

// links reports whether deps, the dependencies of a test binary as go list
// states them, contain the package importPath, or a variant of it that the
// binary's tests recompile.
func links(deps []string, importPath string) bool {
	return slices.ContainsFunc(deps, func(d string) bool {
		return d == importPath || strings.HasPrefix(d, importPath+variant)
	})
}
