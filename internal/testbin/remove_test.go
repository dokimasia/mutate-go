// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package testbin_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/testbin"
)

// closedMode is the mode of a directory without any permission bit.
const closedMode = 0o000

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("RemoveAll", func(t *testing.T) {
		t.Parallel()

		t.Run("removes a tree whose directories lack the owner's write and search bits", func(t *testing.T) {
			t.Parallel()
			root := files.Workspace(t, files.Tree{
				"tmp/locked/file":       files.Text("a test's data\n"),
				"tmp/locked":            files.Dir().WithMode(lockedMode),
				"tmp/closed/inner/file": files.Text("more data\n"),
				"tmp/closed":            files.Dir().WithMode(closedMode),
			})
			assert.NoError(t, testbin.RemoveAll(filepath.Join(root, "tmp")), "RemoveAll removes the tree")
			files.Equal(t, os.DirFS(root), files.Tree{}, "and nothing of the tree remains")
		})

		t.Run("removes a directory that lacks the owner's bits itself", func(t *testing.T) {
			t.Parallel()
			root := files.Workspace(t, files.Tree{
				"tmp/file": files.Text("a test's data\n"),
				"tmp":      files.Dir().WithMode(closedMode),
			})
			assert.NoError(t, testbin.RemoveAll(filepath.Join(root, "tmp")), "RemoveAll removes the directory")
			files.Equal(t, os.DirFS(root), files.Tree{}, "and nothing of it remains")
		})

		t.Run("returns nil for a path where nothing is", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, testbin.RemoveAll(filepath.Join(t.TempDir(), "missing")), "nothing is left to remove")
		})

		t.Run("keeps the mode of a file outside the tree that a link of the tree names", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == windowsOS {
				t.Skip("Windows records no permission bits of a file")
			}
			root := files.Workspace(t, files.Tree{
				"outside":  files.Text("a file of the user\n").WithMode(fileMode),
				"tmp/link": files.Link("../outside"),
			})
			assert.NoError(t, testbin.RemoveAll(filepath.Join(root, "tmp")), "RemoveAll removes the tree with the link")
			files.HasMode(t, filepath.Join(root, "outside"), fileMode,
				"and leaves the file that the link names as it was")
		})

		t.Run("returns the error of a directory whose parent lacks the owner's write bit", func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == windowsOS {
				t.Skip("Windows does not honour the permission bits of a directory")
			}
			root := files.Workspace(t, files.Tree{
				"parent/child/file": files.Text("a test's data\n"),
				"parent":            files.Dir().WithMode(lockedMode),
			})
			child := filepath.Join(root, "parent", "child")
			err := testbin.RemoveAll(child)
			assert.ErrorIs(t, err, fs.ErrPermission, "the parent keeps the directory")
			assert.HasPrefix(t, err.Error(), "testbin: ", "the error starts with the package's name")
			files.IsDir(t, child, "so the directory remains")
		})
	})
}
