// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package memory_test

import "testing/fstest"

// files returns a file system of the files by path.
func files(texts map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, text := range texts {
		fsys[name] = &fstest.MapFile{Data: []byte(text)}
	}
	return fsys
}
