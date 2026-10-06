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
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/enumerate"
	"go.dokimi.dev/mutate/internal/load"
)

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
}

// Render instruments every site of r that has a runnable mutant.
//
// Render type-checks the instrumented package. When the type checker
// rejects a site's form, Render marks each runnable mutant of the site
// NotViable, with the type checker's message as its reason, and
// instruments the package again without the site.
//
// # Errors
//
// Render returns an error when the type checker rejects the instrumented
// package outside every form, or when a file name for the helper file is
// in use.
func Render(p *load.Package, r *enumerate.Result) (*Program, error) {
	name, err := helperName(p.Dir)
	if err != nil {
		return nil, err
	}
	lift := p.Language > 0 && p.Language < 18
	for {
		prog, spans := build(p, r, filepath.Join(p.Dir, name), lift)
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
		name := "zz_mutate.go"
		if i > 0 {
			name = "zz_mutate_" + strconv.Itoa(i) + ".go"
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

// build instruments the sites of r that have a runnable mutant, and
// returns the program and the ranges of its forms by file path.
func build(p *load.Package, r *enumerate.Result, helperPath string, lift bool) (*Program, map[string][]span) {
	prog := &Program{Files: map[string][]byte{}, Ordinals: map[*enumerate.Mutant]int{}}
	first := map[*enumerate.Site]int{}
	byFile := map[*load.File][]*enumerate.Site{}
	ordinal := 0
	for _, s := range r.Sites {
		if !runnable(s) {
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
		prog.Files[f.Path], spans[f.Path] = renderFile(p.Fset, f, sites, first, lift)
	}
	prog.Files[helperPath], spans[helperPath] = helper(p.Name, prog.Sites, first, ordinal, lift)
	return prog, spans
}

func runnable(s *enumerate.Site) bool {
	for _, m := range s.Mutants {
		if m.Status == enumerate.Runnable {
			return true
		}
	}
	return false
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
	paths := make([]string, 0, len(prog.Files))
	for path := range prog.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		f, err := parser.ParseFile(p.Fset, path, prog.Files[path], parser.ParseComments|parser.SkipObjectResolution)
		var list scanner.ErrorList
		if errors.As(err, &list) {
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
		language = 18
	}
	for _, e := range p.Check(files, language) {
		if strings.HasPrefix(e.Msg, "\t") {
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

// Write writes each file of the program into its own directory under dir,
// and an overlay that maps each file's path to its copy. It returns the
// overlay's path.
func (prog *Program) Write(dir string) (string, error) {
	paths := make([]string, 0, len(prog.Files))
	for path := range prog.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	replace := map[string]string{}
	for i, path := range paths {
		copyPath := filepath.Join(dir, strconv.Itoa(i), filepath.Base(path))
		if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
		if err := os.WriteFile(copyPath, prog.Files[path], 0o644); err != nil {
			return "", fmt.Errorf("render: %w", err)
		}
		replace[path] = copyPath
	}
	overlay, _ := json.Marshal(map[string]map[string]string{"Replace": replace})
	path := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(path, overlay, 0o644); err != nil {
		return "", fmt.Errorf("render: %w", err)
	}
	return path, nil
}
