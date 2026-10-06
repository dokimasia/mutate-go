// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package selection

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Var is the variable of the environment that states the selection of
// mutate.Check: entries file:first-last separated by commas, each file
// relative to the package directory.
const Var = "DOKIMI_MUTATE_LINES"

// The separators of an entry and of a list of entries.
const (
	// pathEnd ends the path of an entry: the last colon, so a path may
	// contain a colon.
	pathEnd = ":"
	// rangeSeparator separates an entry's first line from its last.
	rangeSeparator = "-"
	// entrySeparator separates the entries of a list.
	entrySeparator = ","
)

// Lines selects the lines First to Last of the file at Path, an absolute
// path.
type Lines struct {
	Path        string
	First, Last int
}

// ParseEntry parses an entry of a selection, file:first-last, whose file is
// relative to the working directory, and returns the lines with the file's
// absolute path. The file ends at the entry's last colon.
//
// # Errors
//
// ParseEntry returns an error for an entry without a file or a range, and
// for a range that does not start at line 1 or later and end at or after
// its start.
func ParseEntry(entry string) (Lines, error) {
	colon := strings.LastIndex(entry, pathEnd)
	first, last, ok := strings.Cut(entry[colon+1:], rangeSeparator)
	from, err1 := strconv.Atoi(first)
	to, err2 := strconv.Atoi(last)
	if colon < 1 || !ok || err1 != nil || err2 != nil || from < 1 || to < from {
		return Lines{}, fmt.Errorf("selection: %q is not file:first-last with 1 <= first <= last", entry)
	}
	// Abs fails only without a working directory, in which the run cannot
	// load its package either.
	path, _ := filepath.Abs(entry[:colon])
	return Lines{Path: path, First: from, Last: to}, nil
}

// ParseList parses entries separated by commas, as Var states them, and
// returns their lines in order. It returns nil for an empty list, which
// selects every line.
//
// # Errors
//
// ParseList returns the error of the first entry that does not parse, as
// ParseEntry states it.
func ParseList(list string) ([]Lines, error) {
	if list == "" {
		return nil, nil
	}
	var lines []Lines
	for entry := range strings.SplitSeq(list, entrySeparator) {
		l, err := ParseEntry(entry)
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, nil
}
