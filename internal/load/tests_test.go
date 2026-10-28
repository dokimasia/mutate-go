// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"io/fs"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/load"
)

func TestTests(t *testing.T) {
	t.Parallel()

	t.Run("Declarations", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the file of each test, fuzz test and example that the files declare", func(t *testing.T) {
			t.Parallel()
			dir := files.Workspace(t, files.Tree{
				"a_test.go": files.Text("package fixture\n\nfunc Test(t *T) {}\n\nfunc TestA(t *T) {}\n\n" +
					"func Testing(t *T) {}\n\nfunc helper() {}\n"),
				"b_test.go": files.Text("package fixture_test\n\nfunc TestÄ(t *T) {}\n\nfunc FuzzB(f *F) {}\n\n" +
					"func ExampleC() {}\n\nfunc BenchmarkD(b *B) {}\n\ntype suite struct{}\n\nfunc (suite) TestE(t *T) {}\n"),
				"c_test.go": files.Text("package fixture\n\nfunc TestF(t *T) {}\n"),
			})
			got, err := load.Declarations(dir, []string{"a_test.go", "b_test.go"})
			assert.NoError(t, err, "the test files parse")
			assert.Equal(t, got, map[string]string{
				"Test": "a_test.go", "TestA": "a_test.go",
				"TestÄ": "b_test.go", "FuzzB": "b_test.go", "ExampleC": "b_test.go",
			}, "the map contains the functions that go test runs, from the files that the caller names")
		})

		t.Run("returns the first file that declares a test", func(t *testing.T) {
			t.Parallel()
			dir := files.Workspace(t, files.Tree{
				"a_test.go": files.Text("package fixture_test\n\nfunc TestA(t *T) {}\n"),
				"b_test.go": files.Text("package fixture\n\nfunc TestA(t *T) {}\n"),
			})
			got, err := load.Declarations(dir, []string{"b_test.go", "a_test.go"})
			assert.NoError(t, err, "the test files parse")
			assert.Equal(t, got, map[string]string{"TestA": "b_test.go"},
				"the file that comes first in files declares TestA")
		})

		t.Run("returns an error for a test file that does not parse", func(t *testing.T) {
			t.Parallel()
			dir := files.Workspace(t, files.Tree{"a_test.go": files.Text("package fixture\n\nfunc {\n")})
			_, err := load.Declarations(dir, []string{"a_test.go"})
			assert.HasError(t, err, "the test file does not parse")
			assert.Contains(t, err.Error(), "a_test.go", "the error contains the file's name")
		})

		t.Run("returns an error that wraps fs.ErrNotExist for a test file that does not exist", func(t *testing.T) {
			t.Parallel()
			_, err := load.Declarations(t.TempDir(), []string{"a_test.go"})
			assert.ErrorIs(t, err, fs.ErrNotExist, "the test file does not read")
		})
	})
}
