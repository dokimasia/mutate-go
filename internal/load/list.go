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
	"reflect"
	"strings"
)

// The arguments of go list that the package passes.
const (
	// listCommand is the go command's subcommand.
	listCommand = "list"
	// listErrors makes go list state the error of a package in its entry,
	// and list the other packages, instead of failing.
	listErrors = "-e"
	// listJSON writes each entry as JSON with the fields that follow it,
	// separated by commas.
	listJSON = "-json="
	// listExport, listDeps and listCompiled add the export data, the
	// dependencies and the compiled files that Load reads.
	listExport   = "-export"
	listDeps     = "-deps"
	listCompiled = "-compiled"
	// listTests adds the test binaries that Linking reads.
	listTests = "-test"
	// workingPackage is the pattern of the package in the working
	// directory.
	workingPackage = "."
)

// Listed is one entry of go list: a package, its directory, and the errors
// that go list states of it.
type Listed struct {
	// ImportPath is the package's import path, or the pattern that names no
	// package.
	ImportPath string
	// Dir is the package directory, with symbolic links resolved.
	Dir string
	// Error is the error of the package itself, such as that of a pattern
	// that names no directory, or nil.
	Error *ListError
	// DepsErrors lists the errors of the package's dependencies.
	DepsErrors []*ListError
}

// ListError is one error that go list states of a package.
type ListError struct {
	Err string
}

// List lists the packages that patterns name, as go list resolves them in
// cfg.Dir with the environment cfg.Env, in the order that go list writes
// them.
//
// # Errors
//
// List returns an error when the go command fails or writes an entry that
// does not decode. go list states the error of a package that does not
// list in the package's entry, which List returns in Listed.Error.
func List(ctx context.Context, cfg Config, patterns []string) ([]Listed, error) {
	return list[Listed](ctx, cfg.Dir, cfg.Env, patterns...)
}

// Problem returns the first error that go list states of l, without the
// white space around it: the package's own error, or else the first error
// of its dependencies. It returns an empty string when go list states no
// error.
func (l *Listed) Problem() string {
	if problems := l.problems(); len(problems) > 0 {
		return problems[0]
	}
	return ""
}

// problems returns every error that go list states of l: the package's own
// first, and then those of its dependencies, each without the white space
// around it.
func (l *Listed) problems() []string {
	var problems []string
	if l.Error != nil {
		problems = append(problems, strings.TrimSpace(l.Error.Err))
	}
	for _, e := range l.DepsErrors {
		problems = append(problems, strings.TrimSpace(e.Err))
	}
	return problems
}

// list runs go list -e in dir with the environment env, the JSON fields of
// T and args, and decodes each entry that go list writes into a T. It
// returns an error when the go command fails or an entry does not decode.
func list[T any](ctx context.Context, dir string, env []string, args ...string) ([]T, error) {
	head := []string{listCommand, listErrors, listJSON + strings.Join(fieldNames[T](), ",")}
	out, err := Go(ctx, dir, env, append(head, args...)...)
	if err != nil {
		return nil, err
	}
	var entries []T
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var e T
		if err := dec.Decode(&e); errors.Is(err, io.EOF) {
			return entries, nil
		} else if err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		entries = append(entries, e)
	}
}

// fieldNames returns the names of the exported fields of the struct type
// T, in their order, with the fields that an embedded struct promotes in
// place of that struct. They are the names of the fields of a go list
// entry, and of the variables of go env, that the go command writes.
func fieldNames[T any]() []string {
	var names []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[T]()) {
		if f.IsExported() && !f.Anonymous {
			names = append(names, f.Name)
		}
	}
	return names
}
