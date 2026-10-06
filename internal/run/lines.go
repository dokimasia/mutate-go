// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseLines parses an entry of a selection, file:first-last, whose file is
// relative to the working directory. It returns an error for an entry
// without a file or a range, and for a range that does not start at line 1
// or later and end at or after its start.
func ParseLines(entry string) (Lines, error) {
	colon := strings.LastIndex(entry, ":")
	first, last, ok := strings.Cut(entry[colon+1:], "-")
	from, err1 := strconv.Atoi(first)
	to, err2 := strconv.Atoi(last)
	if colon < 1 || !ok || err1 != nil || err2 != nil || from < 1 || to < from {
		return Lines{}, fmt.Errorf("run: %q is not file:first-last with 1 <= first <= last", entry)
	}
	// Abs fails only without a working directory, in which the run cannot
	// load its package either.
	path, _ := filepath.Abs(entry[:colon])
	return Lines{Path: path, First: from, Last: to}, nil
}
