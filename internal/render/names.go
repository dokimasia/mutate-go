// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
)

// namePrefix starts each name of the instrumentation in the renderer's own
// text: the helper file's declarations and imports, the calls of the forms,
// and the variables that a Zero form returns. named writes the package's
// prefix in its place.
const namePrefix = "_mutate"

// testSuffix ends the name of a test file.
const testSuffix = "_test.go"

// prefixOf returns the first of _mutate, _mutate1, _mutate2 and so on with
// which no identifier of p starts. It reads the identifiers of each file
// that the compiler compiles for the package and of each test file in the
// package directory, whatever its build constraints. Every name of the
// instrumentation starts with the prefix, so no identifier of the package or
// of its tests is one of them, and no declaration of either hides one or
// takes its place.
//
// # Errors
//
// prefixOf returns an error when the package directory does not list or a
// test file does not read.
func prefixOf(p *load.Package) (string, error) {
	var used []string
	for _, f := range p.Files {
		used = identifiers(used, f.Text)
	}
	entries, err := os.ReadDir(p.Dir)
	if err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), testSuffix) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(p.Dir, e.Name()))
		if err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
		used = identifiers(used, text)
	}
	for n := 0; ; n++ {
		prefix := namePrefix
		if n > 0 {
			prefix += strconv.Itoa(n)
		}
		if !slices.ContainsFunc(used, func(id string) bool { return strings.HasPrefix(id, prefix) }) {
			return prefix, nil
		}
	}
}

// identifiers appends to used each identifier of the Go source text that
// starts with namePrefix. It reads the tokens of text that does not parse
// too, and no comment.
func identifiers(used []string, text []byte) []string {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(text)), text, nil, 0)
	for {
		_, tok, lit := s.Scan()
		switch {
		case tok == token.EOF:
			return used
		case tok == token.IDENT && strings.HasPrefix(lit, namePrefix):
			used = append(used, lit)
		}
	}
}

// named returns text, a constant of the renderer whose names of the
// instrumentation start with namePrefix, with prefix in place of
// namePrefix. No other part of such a constant contains namePrefix. named
// receives only the renderer's constants, never the package's code, so
// every identifier of the package keeps its name.
func named(prefix, text string) string {
	if prefix == namePrefix {
		return text
	}
	return strings.ReplaceAll(text, namePrefix, prefix)
}
