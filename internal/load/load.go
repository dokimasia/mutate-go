// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"go/version"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// The arguments of go env that Load passes: the subcommand, and the flag
// that writes the variables that follow it as one JSON object.
const (
	envCommand = "env"
	envJSON    = "-json"
)

// compiler is the toolchain whose export data and sizes the type checker
// reads.
const compiler = "gc"

// The prefixes of a Go version as go/version and go/types spell it, such as
// go1.21: the toolchain's name, and the name with the major version 1.
const (
	goPrefix       = "go"
	languagePrefix = goPrefix + "1."
)

// moduleLanguage is the language version of a module without a go line,
// which the go command compiles as go 1.16.
const moduleLanguage = 16

// Package is one type-checked package and the files that the compiler
// compiles for it.
type Package struct {
	// ImportPath is the package's import path.
	ImportPath string
	// Name is the package clause's name.
	Name string
	// Dir is the package directory, with symbolic links resolved.
	Dir string
	// Root is the directory of the package's module, or Dir for a package
	// outside a module. Every file's Name is relative to it.
	Root string
	// Language is N in the language version go1.N that the go command
	// compiles the package's files with, from the module's go line. It is 0
	// for a package outside a module, whose files the go command compiles
	// with the toolchain's own language version.
	Language int
	// Toolchain is the go command's version, as go env GOVERSION reports
	// it.
	Toolchain string
	// Tests reports whether the package has a test file, without which go
	// test -c does not write a test binary.
	Tests bool
	// Files lists every file that the compiler compiles for the package,
	// in the order that go list reports them.
	Files []*File
	Fset  *token.FileSet
	Types *types.Package
	// Info states the types, the definitions, the uses and the scopes of
	// every file in Files.
	Info *types.Info

	importer types.Importer
	sizes    types.Sizes
}

// Role is what a compiled file is to the engine.
type Role int

const (
	// Source is a file of the package's own code, which the engine
	// mutates.
	Source Role = iota
	// Cgo is cgo's rewrite of a file that imports C. Its line directives
	// map its positions to the file that imports C, and the engine lists
	// its sites as skipped.
	Cgo
	// Support is a file that cgo writes for the package. The type checker
	// reads it, and the engine reads nothing else from it.
	Support
)

// File is one compiled file.
type File struct {
	Role Role
	// Generated reports whether [go/ast.IsGenerated] reports the file's
	// source, and no comment before its package clause is the line
	// [Config.Include]. The engine mutates a generated Source file, and
	// lists the sites of a generated Cgo file, only in a run that includes
	// generated files. A Support file is never Generated.
	Generated bool
	// Path is the absolute path of the file that the type checker read.
	Path string
	// Name is the path of the file's source, relative to the package's
	// Root, with / as the separator. For a Cgo file the source is the file
	// that imports C, and a Support file has no Name.
	Name   string
	Text   []byte
	Syntax *ast.File
}

// Config states the package to load and the environment of the go
// command.
type Config struct {
	// Dir is the package directory.
	Dir string
	// Env is the environment of every go command, as os.Environ returns
	// it. The go command is the first one that Env's PATH names.
	Env []string
	// Imports lists the import paths of the packages whose export data
	// Check reads besides the package's dependencies: the imports of a file
	// that the caller adds to the package.
	Imports []string
	// Include is the line comment, such as //dokimi:mutate-include, that
	// makes a generated file a Source file when it is a comment before the
	// file's package clause. Empty makes every generated file Generated.
	Include string
}

// goEnv is the part of go env's output that Load reads. Its field names are
// the names of the variables that Load asks go env for.
type goEnv struct {
	GOVERSION  string
	GOMODCACHE string
	GOARCH     string
}

// listedPackage is the part of a go list entry that Load reads.
type listedPackage struct {
	Listed
	Name            string
	Export          string
	GoFiles         []string
	CgoFiles        []string
	CompiledGoFiles []string
	TestGoFiles     []string
	XTestGoFiles    []string
	ImportMap       map[string]string
	DepOnly         bool
	Module          *struct {
		Dir       string
		GoVersion string
	}
}

// Load lists the package in cfg.Dir with go list, and type-checks the
// files that go list reports in CompiledGoFiles against the export data of
// the package's dependencies.
//
// # Errors
//
// Load returns an error when the go command fails or its version differs
// from the version of the toolchain that built the engine, whose importer
// reads only export data of its own version. It also returns an error when
// the package lies in the module cache, whose files the go command does not
// replace, when go list reports an error of the package or of a
// dependency, and when a file does not parse or type-check. The error
// states the cause, each error that go list reports once, and its text does
// not start with the name of this package.
//
// When go list lists the package and Load still fails, Load returns the
// package with the error, as go/types returns a package that does not
// type-check: its ImportPath, Name, Dir, Root, Language, Toolchain and
// Tests are set.
func Load(ctx context.Context, cfg Config) (*Package, error) {
	dir, err := filepath.Abs(cfg.Dir)
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		return nil, err
	}
	out, err := Go(ctx, dir, cfg.Env, append([]string{envCommand, envJSON}, fieldNames[goEnv]()...)...)
	if err != nil {
		return nil, err
	}
	var env goEnv
	if err = json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("go env: %w", err)
	}
	if env.GOVERSION != runtime.Version() {
		return nil, fmt.Errorf(
			"the go command is %s and the engine was built by %s, and export data of one toolchain does not read in the other",
			env.GOVERSION,
			runtime.Version(),
		)
	}
	cache, err := filepath.EvalSymlinks(env.GOMODCACHE)
	if err == nil && within(cache, dir) {
		return nil, fmt.Errorf("%s is in the module cache, whose files the go command does not replace", dir)
	}
	args := append([]string{listExport, listDeps, listCompiled, workingPackage}, cfg.Imports...)
	entries, err := list[listedPackage](ctx, dir, cfg.Env, args...)
	if err != nil {
		return nil, err
	}
	target, exports, err := find(entries, dir)
	if target == nil {
		return nil, err
	}
	p := &Package{
		ImportPath: target.ImportPath, Name: target.Name, Dir: dir, Root: dir, Toolchain: env.GOVERSION,
		Tests: len(target.TestGoFiles)+len(target.XTestGoFiles) > 0, Fset: token.NewFileSet(),
	}
	if target.Module != nil {
		p.Root = target.Module.Dir
		p.Language = language(target.Module.GoVersion)
	}
	if err != nil {
		return p, err
	}
	if err := p.parse(target, cfg.Include); err != nil {
		return p, err
	}
	if err := p.check(target, exports, env.GOARCH); err != nil {
		return p, err
	}
	return p, nil
}

// find returns the entry of the package in dir, the export data of every
// entry by import path, and the errors that go list states of the entries,
// each once. The entry is nil, and the error states it, when go list lists
// no package in dir.
func find(entries []listedPackage, dir string) (*listedPackage, map[string]string, error) {
	var target *listedPackage
	exports := map[string]string{}
	var problems []string
	for i := range entries {
		e := &entries[i]
		exports[e.ImportPath] = e.Export
		for _, problem := range e.problems() {
			if !slices.Contains(problems, problem) {
				problems = append(problems, problem)
			}
		}
		if !e.DepOnly && resolves(e.Dir, dir) {
			target = e
		}
	}
	if len(problems) > 0 {
		return target, exports, errors.New(strings.Join(problems, "\n"))
	}
	if target == nil {
		return nil, nil, fmt.Errorf("go list lists no package in %s", dir)
	}
	return target, exports, nil
}

// language returns N of the go line 1.N[.P], and moduleLanguage for a
// module without a go line.
func language(goLine string) int {
	if goLine == "" {
		return moduleLanguage
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(version.Lang(goPrefix+goLine), languagePrefix))
	return n
}

// parse reads and parses every compiled file, gives each its role, and
// marks each generated file whose header does not contain the line include.
func (p *Package) parse(target *listedPackage, include string) error {
	sources := map[string]bool{}
	for _, name := range target.GoFiles {
		sources[name] = true
	}
	cgo := map[string]bool{}
	for _, name := range target.CgoFiles {
		cgo[filepath.Join(p.Dir, name)] = true
	}
	for _, name := range target.CompiledGoFiles {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Dir, name)
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		syntax, err := parser.ParseFile(p.Fset, path, text, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		f := &File{Role: Support, Path: path, Text: text, Syntax: syntax}
		source := path
		switch {
		case sources[name]:
			f.Role = Source
			f.Generated = generated(syntax, include)
		case cgo[p.Fset.Position(syntax.Package).Filename]:
			// The line directives of cgo's rewrite name the file that
			// imports C, whose header decides whether it is generated.
			source = p.Fset.Position(syntax.Package).Filename
			f.Role = Cgo
			header, err := parser.ParseFile(
				token.NewFileSet(),
				source,
				nil,
				parser.PackageClauseOnly|parser.ParseComments,
			)
			if err != nil {
				return err
			}
			f.Generated = generated(header, include)
		}
		if f.Role != Support {
			rel, err := filepath.Rel(p.Root, source)
			if err != nil {
				return err
			}
			f.Name = filepath.ToSlash(rel)
		}
		p.Files = append(p.Files, f)
	}
	return nil
}

// generated reports whether [go/ast.IsGenerated] reports f, and no comment
// before f's package clause is the line include.
func generated(f *ast.File, include string) bool {
	if !ast.IsGenerated(f) {
		return false
	}
	for _, group := range f.Comments {
		if group.Pos() >= f.Package {
			break
		}
		for _, c := range group.List {
			if c.Text == include {
				return false
			}
		}
	}
	return true
}

// check type-checks the parsed files against the export data of the
// package's dependencies.
func (p *Package) check(target *listedPackage, exports map[string]string, arch string) error {
	lookup := func(path string) (io.ReadCloser, error) {
		if mapped, ok := target.ImportMap[path]; ok {
			path = mapped
		}
		export := exports[path]
		if export == "" {
			return nil, fmt.Errorf("no export data for %q", path)
		}
		return os.Open(export)
	}
	p.importer = importer.ForCompiler(p.Fset, compiler, lookup)
	p.sizes = types.SizesFor(compiler, arch)
	p.Info = &types.Info{
		Types:  map[ast.Expr]types.TypeAndValue{},
		Defs:   map[*ast.Ident]types.Object{},
		Uses:   map[*ast.Ident]types.Object{},
		Scopes: map[ast.Node]*types.Scope{},
	}
	syntax := make([]*ast.File, len(p.Files))
	for i, f := range p.Files {
		syntax[i] = f.Syntax
	}
	var problems []string
	conf := p.config(p.Language, func(err error) { problems = append(problems, err.Error()) })
	p.Types, _ = conf.Check(p.ImportPath, p.Fset, syntax, p.Info)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

// config returns the type checker's configuration for the package at the
// language version go1.N, where N is language, or the toolchain's own for
// 0. report receives every error.
func (p *Package) config(language int, report func(error)) *types.Config {
	conf := &types.Config{Importer: p.importer, Sizes: p.sizes, Error: report}
	if language > 0 {
		conf.GoVersion = languagePrefix + strconv.Itoa(language)
	}
	return conf
}

// Check type-checks files, which the caller parses into Fset, as the
// package at the language version go1.N, where N is language, or the
// toolchain's own for 0. It reads the dependencies from the export data
// that Load read, and returns every error that the type checker reports.
func (p *Package) Check(files []*ast.File, language int) []types.Error {
	var errs []types.Error
	conf := p.config(language, func(err error) {
		// The type checker reports every error as a types.Error.
		if e, ok := errors.AsType[types.Error](err); ok {
			errs = append(errs, e)
		}
	})
	_, _ = conf.Check(p.ImportPath, p.Fset, files, nil)
	return errs
}

// resolves reports whether path, with its symbolic links resolved, is dir.
func resolves(path, dir string) bool {
	path, err := filepath.EvalSymlinks(path)
	return err == nil && path == dir
}

// within reports whether path is dir or lies beneath it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
