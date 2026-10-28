// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package load

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// testPrefixes start the names of the functions that go test runs as tests:
// a test, a fuzz test and an example.
var testPrefixes = []string{"Test", "Fuzz", "Example"}

// Declarations returns the file that declares each test of files, the
// names of test files of the package in dir, by the test's name. A test is
// a top-level function whose name go test runs as a test, a fuzz test or an
// example: the prefix alone, or the prefix and a name that does not start
// with a lower-case letter. A test that two files declare maps to the first
// of them in files.
//
// # Errors
//
// Declarations returns the error of a file that does not read or parse.
// The error contains the file's path, and its text does not start with the
// name of this package.
func Declarations(dir string, files []string) (map[string]string, error) {
	declarations := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err //nolint:wrapcheck // the error contains the file's path, and Declarations adds no prefix
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !isTest(fn.Name.Name) {
				continue
			}
			if _, ok := declarations[fn.Name.Name]; !ok {
				declarations[fn.Name.Name] = name
			}
		}
	}
	return declarations, nil
}

// isTest reports whether go test runs the function name as a test, a fuzz
// test or an example.
func isTest(name string) bool {
	for _, prefix := range testPrefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		// The rune of an empty rest is utf8.RuneError, which is not lower-case.
		r, _ := utf8.DecodeRuneInString(rest)
		return !unicode.IsLower(r)
	}
	return false
}
