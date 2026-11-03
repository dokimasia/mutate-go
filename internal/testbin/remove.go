// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package testbin

import (
	"fmt"
	"io/fs"
	"os"
)

// unlockMode is the mode that RemoveAll gives each directory of a tree
// before it removes the tree: the owner's read, write and search bits.
const unlockMode = 0o700

// RemoveAll removes dir and everything below it, as os.RemoveAll does, also
// below a directory that a test locked. It first gives dir and each
// directory below it the owner's read, write and search bits, as the go
// command does before it removes a tree of its module cache, because
// os.RemoveAll cannot remove the entries of a directory without the owner's
// write bit. It changes the modes through an os.Root of dir, so a link in
// the tree, or one that a process puts in place of a directory, changes no
// mode outside dir.
//
// Error modes:
//   - the error of the removal, such as for a directory whose parent lacks
//     the owner's write bit. What the removal could not remove remains.
//
// A mode that does not change, such as one of another user's directory,
// makes the removal fail, so RemoveAll leaves the errors of the changes, and
// of the root's close, to the removal.
func RemoveAll(dir string) error {
	_ = os.Chmod(dir, unlockMode)
	if root, err := os.OpenRoot(dir); err == nil {
		// The walk passes a nil entry only when the Stat of the root fails,
		// and the Stat of an open root succeeds.
		_ = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, _ error) error {
			if d.IsDir() {
				_ = root.Chmod(name, unlockMode)
			}
			return nil
		})
		_ = root.Close()
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("testbin: %w", err)
	}
	return nil
}
