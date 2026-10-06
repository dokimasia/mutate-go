// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/spec"
)

// vendored is the directory of the vendored definition, as a path from this
// package's directory.
var vendored = filepath.Join("..", "..", "conformance", "spec")

// semver matches a version as the definition writes one: major, minor and
// patch.
const semver = `^[0-9]+\.[0-9]+\.[0-9]+$`

// The parts of this package's source that declared reads: the suffix of a Go
// file, the suffix of a test file, and the compiler whose export data the
// importer reads for the package's imports.
const (
	goSuffix   = ".go"
	testSuffix = "_test.go"
	compiler   = "gc"
)

func TestSpec(t *testing.T) {
	t.Parallel()

	t.Run("Load", func(t *testing.T) {
		t.Parallel()

		t.Run("returns every field of the embedded files", func(t *testing.T) {
			t.Parallel()
			var want spec.Definition
			strict(t, spec.CatalogueFile, &want.Catalogue)
			strict(t, spec.ProtocolFile, &want.Protocol)
			strict(t, spec.OverlayFile, &want.Overlay)
			version, err := os.ReadFile(spec.VersionFile)
			assert.NoError(t, err, "the version file reads")
			want.Version = strings.TrimSpace(string(version))
			assert.Equal(t, spec.Load(), want, "Load decodes each embedded file into its type")
		})

		t.Run("returns the files of the vendored definition", func(t *testing.T) {
			t.Parallel()
			for _, name := range []string{spec.VersionFile, spec.CatalogueFile, spec.ProtocolFile, spec.OverlayFile} {
				ours, err := os.ReadFile(name)
				assert.NoError(t, err, "the embedded copy of "+name+" reads")
				theirs, err := os.ReadFile(filepath.Join(vendored, name))
				assert.NoError(t, err, "the vendored "+name+" reads")
				expect.Equal(
					t,
					string(ours),
					string(theirs),
					"the copy of "+name+" is the vendored file, so make spec-sync ran",
				)
			}
		})

		t.Run("decodes the files once", func(t *testing.T) {
			t.Parallel()
			first, second := spec.Load(), spec.Load()
			assert.True(t, &first.Catalogue.Kinds[0] == &second.Catalogue.Kinds[0],
				"two calls return the same decoded kinds")
		})

		t.Run("returns the catalogue's semantic version", func(t *testing.T) {
			t.Parallel()
			assert.Matches(t, spec.Load().Version, semver, "the catalogue's version is major.minor.patch")
		})

		t.Run("returns the overlay's semantic version", func(t *testing.T) {
			t.Parallel()
			assert.Matches(t, spec.Load().Overlay.Version, semver, "the overlay's version is major.minor.patch")
		})
	})
}

// strict decodes the file name of this package's directory into v, and
// stops the test when the file has a field that v's type lacks.
func strict(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(name)
	assert.NoError(t, err, "the file reads")
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	assert.NoError(t, dec.Decode(v), name+" decodes into its type with no field left over")
}

// declared returns the value of every constant of the type named typeName
// that this package declares, as the type checker reads the package's
// source files.
func declared(t *testing.T, typeName string) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	assert.NoError(t, err, "the package directory reads")
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, goSuffix) || strings.HasSuffix(name, testSuffix) {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		assert.NoError(t, parseErr, name+" parses")
		files = append(files, file)
	}
	conf := types.Config{Importer: importer.ForCompiler(fset, compiler, nil)}
	pkg, err := conf.Check("spec", fset, files, nil)
	assert.NoError(t, err, "the package type-checks")
	typ := pkg.Scope().Lookup(typeName).Type()
	values := []string{}
	for _, name := range pkg.Scope().Names() {
		if c, ok := pkg.Scope().Lookup(name).(*types.Const); ok && types.Identical(c.Type(), typ) {
			values = append(values, constant.StringVal(c.Val()))
		}
	}
	return values
}
