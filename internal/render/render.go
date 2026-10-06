// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
)

// generics is the minor version of Go 1.18, the first release with the
// generic functions that the helper file declares.
const generics = 18

// The names of the helper file, which the package directory does not
// contain: helperBase and goSuffix, or helperBase, an underscore, a number
// and goSuffix when that name is in use.
const (
	helperBase = "zz_mutate"
	goSuffix   = ".go"
)

// overlayFile is the name of the overlay that Write writes.
const overlayFile = "overlay.json"

// continuation starts a line of a type checker's error that continues the
// error before it.
const continuation = "\t"

// The modes of the directories and the files that Write writes.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// overlay is the overlay that the go command's -overlay flag reads: the
// path of each file of the build, and the path of the file that the build
// reads in its place.
type overlay struct {
	Replace map[string]string
}

// Program is the files that a build reads in place of a package's own: the
// instrumented package that Render returns, or the ordinary build of one
// mutant that Plain returns, whose Ordinals and Sites are empty.
type Program struct {
	// Files maps the path of each file that the build reads from Program
	// to its text: each source file with an instrumented site, and the
	// helper file, which the package directory does not contain.
	Files map[string][]byte
	// Ordinals maps each mutant of an instrumented site to its ordinal,
	// from 1. The mutants of a site have consecutive ordinals.
	Ordinals map[*enumerate.Mutant]int
	// Sites lists the instrumented sites in the order of the enumeration.
	Sites []*enumerate.Site
	// Prefix starts each name that the instrumented package adds: the
	// declarations and imports of the helper file, and the variables that a
	// return of zero values returns. No identifier of the package's files or
	// of its test files starts with it. Plain takes the prefix of the
	// package's instrumented program.
	Prefix string
}

// Render instruments every site of r that has a runnable mutant. The
// instrumented package reads the active mutant's ordinal from the variable
// of the environment that variable names, the protocol's variable, when it
// initializes, and writes its trace to the file that TraceVar names. Each
// name that it adds starts with the first of _mutate, _mutate1, _mutate2
// and so on with which no identifier of the package's files or of its test
// files starts, so no declaration of the package or of its tests hides one
// or takes its place.
//
// Render type-checks the instrumented package. When the type checker
// rejects a site's form, Render marks each runnable mutant of the site
// NotViable, with the type checker's message as its reason, and
// instruments the package again without the site.
//
// # Errors
//
// Render returns an error when the type checker rejects the instrumented
// package outside every form, and when the package directory or one of its
// test files does not read.
func Render(p *load.Package, r *enumerate.Result, variable string) (*Program, error) {
	name, err := helperName(p.Dir)
	if err != nil {
		return nil, err
	}
	prefix, err := prefixOf(p)
	if err != nil {
		return nil, err
	}
	lift := p.Language > 0 && p.Language < generics
	for {
		prog, spans := build(p, r, filepath.Join(p.Dir, name), variable, prefix, lift)
		problems := check(p, prog, lift)
		if len(problems) == 0 {
			return prog, nil
		}
		var unplaced []string
		for _, pr := range problems {
			s := innermost(spans[pr.file], pr.offset)
			if s == nil {
				unplaced = append(unplaced, pr.message)
				continue
			}
			for _, m := range s.Mutants {
				if m.Status == enumerate.Runnable {
					m.Status, m.Reason = enumerate.NotViable, pr.message
				}
			}
		}
		if len(unplaced) > 0 {
			return nil, fmt.Errorf(
				"render: the type checker rejects the instrumented package: %s",
				strings.Join(unplaced, "; "),
			)
		}
	}
}

// helperName returns the first of zz_mutate.go, zz_mutate_1.go and so on
// that dir does not contain.
func helperName(dir string) (string, error) {
	for i := 0; ; i++ {
		name := helperBase + goSuffix
		if i > 0 {
			name = helperBase + "_" + strconv.Itoa(i) + goSuffix
		}
		_, err := os.Lstat(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
	}
}

// build instruments the sites of r that have a runnable mutant, with the
// helper file at helperPath, which reads the protocol's variable that
// variable names, and each name of the instrumentation starting with
// prefix. It returns the program and the ranges of its forms by file path.
func build(
	p *load.Package,
	r *enumerate.Result,
	helperPath, variable, prefix string,
	lift bool,
) (*Program, map[string][]span) {
	prog := &Program{Files: map[string][]byte{}, Ordinals: map[*enumerate.Mutant]int{}, Prefix: prefix}
	first := map[*enumerate.Site]int{}
	byFile := map[*load.File][]*enumerate.Site{}
	ordinal := 0
	for _, s := range r.Sites {
		if !slices.ContainsFunc(s.Mutants, func(m *enumerate.Mutant) bool { return m.Status == enumerate.Runnable }) {
			continue
		}
		prog.Sites = append(prog.Sites, s)
		first[s] = ordinal + 1
		for _, m := range s.Mutants {
			ordinal++
			prog.Ordinals[m] = ordinal
		}
		byFile[s.File] = append(byFile[s.File], s)
	}
	spans := map[string][]span{}
	for f, sites := range byFile {
		prog.Files[f.Path], spans[f.Path] = renderFile(p.Fset, f, sites, first, prefix, lift)
	}
	prog.Files[helperPath], spans[helperPath] = helper(p.Name, variable, prog.Sites, first, ordinal, prefix, lift)
	return prog, spans
}

// problem is one error of the parser or the type checker, at an offset of
// a file of the program.
type problem struct {
	file    string
	offset  int
	message string
}

// check parses the program's files and type-checks them with the package's
// other files, and returns every error.
func check(p *load.Package, prog *Program, lift bool) []problem {
	var problems []problem
	var files []*ast.File
	for _, f := range p.Files {
		if _, ok := prog.Files[f.Path]; !ok {
			files = append(files, f.Syntax)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(prog.Files)) {
		f, err := parser.ParseFile(p.Fset, path, prog.Files[path], parser.ParseComments|parser.SkipObjectResolution)
		if list, ok := errors.AsType[scanner.ErrorList](err); ok {
			for _, e := range list {
				problems = append(problems, problem{file: path, offset: e.Pos.Offset, message: e.Msg})
			}
			continue
		}
		files = append(files, f)
	}
	if len(problems) > 0 {
		return problems
	}
	language := p.Language
	if lift {
		language = generics
	}
	for _, e := range p.Check(files, language) {
		if strings.HasPrefix(e.Msg, continuation) {
			continue
		}
		pos := p.Fset.PositionFor(e.Pos, false)
		problems = append(problems, problem{file: pos.Filename, offset: pos.Offset, message: e.Msg})
	}
	return problems
}

// innermost returns the site of the shortest span that contains offset,
// or nil when no span contains it.
func innermost(spans []span, offset int) *enumerate.Site {
	var best *span
	for i := range spans {
		s := &spans[i]
		if s.start <= offset && offset < s.end && (best == nil || s.end-s.start < best.end-best.start) {
			best = s
		}
	}
	if best == nil {
		return nil
	}
	return best.site
}

// Write writes each file of the program into a directory of its own under
// dir, numbered in the order of the files' paths, and the overlay that maps
// each file's path to its copy. It returns the overlay's path.
//
// # Errors
//
// Write returns an error when a directory or a file does not write.
func (prog *Program) Write(dir string) (string, error) {
	replace := map[string]string{}
	for i, path := range slices.Sorted(maps.Keys(prog.Files)) {
		copyPath := filepath.Join(dir, strconv.Itoa(i), filepath.Base(path))
		if err := os.MkdirAll(filepath.Dir(copyPath), dirMode); err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
		if err := os.WriteFile(copyPath, prog.Files[path], fileMode); err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
		replace[path] = copyPath
	}
	// An overlay contains strings alone, which encoding/json encodes without
	// an error.
	data, _ := json.Marshal(&overlay{Replace: replace})
	path := filepath.Join(dir, overlayFile)
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return path, nil
}
