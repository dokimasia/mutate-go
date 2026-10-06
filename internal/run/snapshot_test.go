// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/run"
	"go.dokimi.dev/mutate/internal/spec"
)

// danglingName is the name of a link to the file missing, which does not
// read.
const danglingName = "dangling"

// The directory of a package's test data, and the name and the mode of a
// directory in it that no user but root reads.
const (
	testdataDir = "testdata"
	lockedName  = "locked"
	lockedMode  = 0o000
)

// dataFiles is a package without a mutant, with a data file in testdata, in
// a directory that the go command ignores, and in a directory without a Go
// file.
var dataFiles = map[string]string{
	"a.go":            "package fixture\n",
	"testdata/in.txt": "in",
	"_data/x.go":      "package x",
	"assets/logo.svg": "<svg/>",
}

// writtenTest is a test of add that writes testdata/written.txt into the
// package's directory in every run.
const writtenTest = `package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("testdata/written.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`

// danglingTest is a test of add that makes a link to a missing file in the
// package's directory.
const danglingTest = `package fixture

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	_ = os.Symlink("missing", "dangling")
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`

func TestSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("Run", func(t *testing.T) {
		t.Parallel()

		t.Run("states each data file of the package in the inputs digest", func(t *testing.T) {
			t.Parallel()
			var digests []string
			for _, files := range []map[string]string{
				dataFiles,
				with(dataFiles, map[string]string{"a.go": "package fixture\n\n// A comment.\n"}),
				with(dataFiles, map[string]string{"testdata/in.txt": "out"}),
				with(dataFiles, map[string]string{"_data/x.go": "package y"}),
				with(dataFiles, map[string]string{"assets/logo.svg": "<svg></svg>"}),
				with(dataFiles, map[string]string{"notes.txt": "x"}),
			} {
				digests = append(digests, runIn(t, module(t, files), run.Config{}).Inputs)
			}
			removed := maps.Clone(dataFiles)
			delete(removed, "testdata/in.txt")
			digests = append(digests, runIn(t, module(t, removed), run.Config{}).Inputs)
			assert.NoDuplicates(t, func() ([]string, error) { return digests, nil },
				"each change of a data file changes the digest")
		})

		t.Run("leaves hidden files and the directories of other packages out of the inputs digest", func(t *testing.T) {
			t.Parallel()
			base := runIn(t, module(t, dataFiles), run.Config{}).Inputs
			for _, more := range []map[string]string{
				{".notes": "x"},
				{".git/HEAD": "ref"},
				{"sub/s.go": "package sub"},
				{"sub/s.go": "package sub", "sub/data.txt": "x"},
			} {
				assert.Equal(t, runIn(t, module(t, with(dataFiles, more)), run.Config{}).Inputs, base,
					"the file is no data file of the package")
			}
		})

		t.Run("states in the inputs digest the files that the runs start from", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{addFile: add, addTestFile: writtenTest})
			first := runIn(t, dir, run.Config{})
			second := runIn(t, dir, run.Config{})
			third := runIn(t, dir, run.Config{})
			assert.NotEqual(t, second.Inputs, first.Inputs, "the second run starts from the file that the first wrote")
			assert.Equal(t, third.Inputs, second.Inputs, "the second and the third run start from the same files")
		})

		t.Run("states changed-files with each data file that the runs added, changed or removed", func(t *testing.T) {
			t.Parallel()
			// Every run of the package's own test writes out.txt and data.txt
			// and removes removed.txt. Every run of the other test writes
			// testdata/c.txt into the package conformance.
			dir := module(t, with(scale, map[string]string{
				"data.txt":    "before",
				"removed.txt": "x",
				scaleTestFile: `package fixture

import (
	"os"
	"testing"
)

func TestScaleByOne(t *testing.T) {
	_ = os.WriteFile("out.txt", []byte("x"), 0o644)
	_ = os.WriteFile("data.txt", []byte("after"), 0o644)
	_ = os.Remove("removed.txt")
	if Scale(2, 1) != 2 {
		t.Error("Scale(2, 1) != 2")
	}
}
`,
				conformanceTestFile: `package conformance

import (
	"os"
	"testing"

	"fixture"
)

func TestScale(t *testing.T) {
	_ = os.MkdirAll("testdata", 0o755)
	_ = os.WriteFile("testdata/c.txt", nil, 0o644)
	if fixture.Scale(6, 3) != 18 {
		t.Error("Scale(6, 3) != 18")
	}
}
`,
			}))
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorChangedFiles},
				"the runs changed the packages' files")
			assert.Equal(t, rec.Errors[0].Message, "the runs added, changed or removed these files: "+
				"data.txt, out.txt, removed.txt, conformance/testdata/c.txt",
				"the message names each file by its path in the module")
			assert.Nil(t, rec.Score, "a run that changed files has no score")
		})

		t.Run("states changed-files when a data file does not read after the runs", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a link needs a privilege on Windows")
			}
			rec := runIn(t, module(t, map[string]string{addFile: add, addTestFile: danglingTest}), run.Config{})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorChangedFiles}, "the files do not read")
			assert.Matches(t, rec.Errors[0].Message, `^the files do not read after the runs: open .*/dangling: `,
				"the message states the error of the file that does not read")
		})

		t.Run("states the load error when a data file of a package of the suite does not read", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a link needs a privilege on Windows")
			}
			dir := module(t, scale)
			assert.NoError(t, os.Symlink(missing, filepath.Join(dir, filepath.Dir(conformanceFile), danglingName)),
				"the link is made")
			rec := runIn(t, dir, run.Config{Suite: []string{conformancePattern}})
			assert.Equal(t, codes(rec), []spec.ErrorCode{spec.ErrorLoad}, "the suite's files do not read")
		})

		t.Run("returns an error when a data file of the package does not read", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" {
				t.Skip("a link needs a privilege on Windows")
			}
			dir := module(t, map[string]string{addFile: add})
			assert.NoError(t, os.Symlink(missing, filepath.Join(dir, danglingName)), "the link is made")
			_, err := run.Run(t.Context(), run.Config{Dir: dir, Env: os.Environ()})
			assert.ErrorIs(t, err, fs.ErrNotExist, "the link's target does not exist")
			assert.HasPrefix(t, err.Error(), errorPrefix, "the error starts with the package's name")
		})

		t.Run("returns an error when a directory of the package's data does not read", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("a directory's mode stops no read on Windows or for root")
			}
			dir := module(t, map[string]string{addFile: add, testdataDir + "/in.txt": "in"})
			assert.NoError(t, os.Mkdir(filepath.Join(dir, testdataDir, lockedName), lockedMode),
				"the locked directory is made")
			_, err := run.Run(t.Context(), run.Config{Dir: dir, Env: os.Environ()})
			assert.ErrorIs(t, err, fs.ErrPermission, "the locked directory does not read")
			assert.HasPrefix(t, err.Error(), errorPrefix, "the error starts with the package's name")
		})
	})
}
