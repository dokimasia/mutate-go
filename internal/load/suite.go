// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Linked is a package whose test binary links another package.
type Linked struct {
	ImportPath string
	// Dir is the package directory, where its test binary runs.
	Dir string
}

// listedTest is the part of an entry of go list -test that Linking reads.
type listedTest struct {
	ImportPath string
	Dir        string
	Deps       []string
	Error      *struct{ Err string }
	DepsErrors []*struct{ Err string }
}

// Linking lists the packages that patterns name, as go list resolves them
// in cfg.Dir, whose test binaries link the package importPath, in the order
// of their import paths. The package importPath itself is not listed, and
// neither is a package without a test file, which has no test binary.
//
// # Errors
//
// Linking returns an error when the go command fails, and when go list
// reports an error of a package that patterns name or of one of its
// dependencies. The error starts with the package's import path.
func Linking(ctx context.Context, cfg Config, importPath string, patterns []string) ([]Linked, error) {
	args := append([]string{"list", "-e", "-test", "-json=ImportPath,Dir,Deps,Error,DepsErrors"}, patterns...)
	out, err := Go(ctx, cfg.Dir, cfg.Env, args...)
	if err != nil {
		return nil, err
	}
	listed := map[string]bool{}
	var mains []listedTest
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var e listedTest
		if err := dec.Decode(&e); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		problem := e.Error
		if problem == nil && len(e.DepsErrors) > 0 {
			problem = e.DepsErrors[0]
		}
		if problem != nil {
			return nil, fmt.Errorf("%s: %s", e.ImportPath, strings.TrimSpace(problem.Err))
		}
		// go list -test names the main package of P's test binary P.test.
		if strings.HasSuffix(e.ImportPath, ".test") {
			mains = append(mains, e)
		} else {
			listed[e.ImportPath] = true
		}
	}
	sort.Slice(mains, func(i, j int) bool { return mains[i].ImportPath < mains[j].ImportPath })
	var linked []Linked
	for _, m := range mains {
		p := strings.TrimSuffix(m.ImportPath, ".test")
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
	for _, d := range deps {
		if d == importPath || strings.HasPrefix(d, importPath+" [") {
			return true
		}
	}
	return false
}
