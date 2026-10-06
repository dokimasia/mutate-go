// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package selection

import (
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The lines of a unified diff that ParseDiff reads.
const (
	// oldHeader and newHeader start the headers that name a file's old and
	// new versions.
	oldHeader = "--- "
	newHeader = "+++ "
	// hunkStart starts the header of a hunk, @@ -start,count +start,count @@,
	// and hunkEnd is its last field.
	hunkStart = "@@ "
	hunkEnd   = "@@"
	// newPrefix starts the path of a file's new version.
	newPrefix = "b/"
	// deleted is the path of the new version of a file that the diff
	// deletes.
	deleted = "/dev/null"
)

// The first byte of each line of a hunk: a line that both versions have, a
// line that the new version adds, a line that it removes, and the note of a
// missing final line break.
const (
	keptLine    = ' '
	addedLine   = '+'
	removedLine = '-'
	noteLine    = '\\'
)

// ParseDiff returns the selection that the unified diff text states for
// the new version of each file, such as the output of git diff: each line
// that the diff adds, and the lines on either side of each run of lines
// that it removes, as cargo-mutants' --in-diff selects them. The lines
// beside a run of removed lines are selected whether the hunk shows them as
// context or not, so a diff without context, such as git diff -U0's,
// selects the same lines. The path of a new version, after its b/ prefix,
// is relative to the working directory. A file that the diff deletes
// selects no line, and a diff that selects no line returns an empty
// selection, which selects none.
//
// # Errors
//
// ParseDiff reads each file whose new version the diff states, and returns
// an error when a line that the diff adds or keeps is not the file's line
// at its number, because the diff is then older or newer than the file. It
// also returns an error for a hunk before the first file header, a hunk
// header that does not parse or whose range does not fit an int, a hunk
// with more lines of a version than its header states, a line of a hunk
// without a line's prefix, a diff that ends inside a hunk, and a file that
// does not read.
//
// A line of a version directly after a hunk's last line, or after the note
// of a missing final line break there, is one line more than the hunk's
// header states, and ParseDiff returns an error for it. These lines are
// lines of a version:
//
//   - a line that starts with a space, + or -, other than a file's header
//   - an empty line, which a tool writes for a line that both versions have
//   - a note of a missing final line break
func ParseDiff(text string) ([]Lines, error) {
	d := &diff{files: map[string][]string{}, selected: map[string]map[int]bool{}}
	// The line break that ends the diff ends its last line, and starts none.
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	header := false
	for i := 0; i < len(lines); i++ {
		switch line := lines[i]; {
		case strings.HasPrefix(line, newHeader):
			header, d.path = true, newPath(line[len(newHeader):])
		case strings.HasPrefix(line, hunkStart):
			if !header {
				return nil, fmt.Errorf("selection: the diff has the hunk %q before a file's header", line)
			}
			end, err := d.hunk(lines, i)
			if err != nil {
				return nil, err
			}
			i = end
		}
	}
	return d.selection(), nil
}

// diff is the state of one ParseDiff: the path of the file whose hunks it
// reads, or "" for a file that the diff deletes, the lines of each file that
// it read, and the selected line numbers of each file in the order of the
// file headers.
type diff struct {
	path     string
	files    map[string][]string
	selected map[string]map[int]bool
	order    []string
}

// hunk reads the hunk whose header is lines[at], and returns the index of
// its last line, or of the note of a missing final line break that follows
// that line. It selects the lines that the hunk adds, and the lines of the
// file before and after each run of removed lines. A line of a version after
// the hunk is an error, as a line of a version past either count inside it
// is.
//
// n is the number of the new version's next line. The header's range fits
// an int, so n is between 1 and the range's end plus 1: a line of the new
// version is one of the range's lines, and the line after an empty range
// is the line after its start.
func (d *diff) hunk(lines []string, at int) (int, error) {
	oldCount, start, newCount, err := hunkHeader(lines[at])
	if err != nil {
		return 0, err
	}
	n := start
	if newCount == 0 {
		n++
	}
	afterRemoved := false
	i := at
	for oldCount > 0 || newCount > 0 {
		i++
		if i == len(lines) {
			return 0, fmt.Errorf("selection: the diff ends inside the hunk %q", lines[at])
		}
		// A context line whose space a tool stripped is empty.
		body := lines[i]
		if body == "" {
			body = string(keptLine)
		}
		kind := body[0]
		if kind == keptLine && (oldCount == 0 || newCount == 0) || kind == addedLine && newCount == 0 ||
			kind == removedLine && oldCount == 0 {
			return 0, fmt.Errorf("selection: the hunk %q has more lines than its header states", lines[at])
		}
		switch kind {
		case keptLine:
			if err := d.keep(n, body[1:], afterRemoved); err != nil {
				return 0, err
			}
			n, oldCount, newCount, afterRemoved = n+1, oldCount-1, newCount-1, false
		case addedLine:
			if err := d.keep(n, body[1:], true); err != nil {
				return 0, err
			}
			n, newCount, afterRemoved = n+1, newCount-1, false
		case removedLine:
			if !afterRemoved {
				if err := d.beside(n - 1); err != nil {
					return 0, err
				}
			}
			oldCount, afterRemoved = oldCount-1, true
		case noteLine:
			// \ No newline at end of file
		default:
			return 0, fmt.Errorf("selection: the line %q of the hunk %q has no line's prefix", lines[i], lines[at])
		}
	}
	if afterRemoved {
		// The removed lines end the hunk, so the file's next line, which a
		// hunk without context does not show, follows them.
		if err := d.beside(n); err != nil {
			return 0, err
		}
	}
	// The note of a missing final line break can follow the hunk's last
	// line. A header, the metadata of the next file or the end of the diff
	// follows them, and a line of a version there is a line more than the
	// header states.
	if i+1 < len(lines) && strings.HasPrefix(lines[i+1], string(noteLine)) {
		i++
	}
	if i+1 < len(lines) && versionLine(lines[i+1]) {
		return 0, fmt.Errorf("selection: the hunk %q has more lines than its header states", lines[at])
	}
	return i, nil
}

// versionLine reports whether line can be a line of a hunk: an empty line,
// which is a line that both versions have and whose space a tool stripped,
// a line that starts with the prefix of a line of a version and does not
// start a file's header, or a note of a missing final line break.
func versionLine(line string) bool {
	if line == "" {
		return true
	}
	switch line[0] {
	case keptLine, noteLine:
		return true
	case addedLine:
		return !strings.HasPrefix(line, newHeader)
	case removedLine:
		return !strings.HasPrefix(line, oldHeader)
	}
	return false
}

// file returns the lines of the file whose hunks the diff reads, without
// their line breaks, and reads the file the first time.
func (d *diff) file() ([]string, error) {
	if file, ok := d.files[d.path]; ok {
		return file, nil
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return nil, fmt.Errorf("selection: %w", err)
	}
	file := []string{}
	for line := range strings.Lines(string(data)) {
		file = append(file, strings.TrimSuffix(line, "\n"))
	}
	d.files[d.path] = file
	return file, nil
}

// keep checks that line n of the file is text, and selects it when
// selected is true. n is at least 1.
func (d *diff) keep(n int, text string, selected bool) error {
	file, err := d.file()
	if err != nil {
		return err
	}
	if n > len(file) || file[n-1] != text {
		return fmt.Errorf("selection: the diff states line %d of %s otherwise than the file, so it is out of date",
			n, d.path)
	}
	if selected {
		d.mark(n)
	}
	return nil
}

// beside selects line n, a line before or after a run of removed lines,
// when the file has it. Removed lines at the start or the end of a file
// have no line on that side, and a file that the diff deletes has none.
func (d *diff) beside(n int) error {
	if d.path == "" || n < 1 {
		return nil
	}
	file, err := d.file()
	if err != nil {
		return err
	}
	if n <= len(file) {
		d.mark(n)
	}
	return nil
}

// mark selects line n of the file whose hunks the diff reads, a file that
// the diff does not delete.
func (d *diff) mark(n int) {
	if d.selected[d.path] == nil {
		d.selected[d.path] = map[int]bool{}
		d.order = append(d.order, d.path)
	}
	d.selected[d.path][n] = true
}

// selection returns the selected lines of each file as ranges of
// consecutive lines, by file in the order of the headers. It returns an
// empty selection, not nil, when the diff selects no line.
func (d *diff) selection() []Lines {
	out := []Lines{}
	for _, path := range d.order {
		for _, n := range slices.Sorted(maps.Keys(d.selected[path])) {
			if last := len(out) - 1; last >= 0 && out[last].Path == path && out[last].Last == n-1 {
				out[last].Last = n
				continue
			}
			out = append(out, Lines{Path: path, First: n, Last: n})
		}
	}
	return out
}

// newPath returns the absolute path that the rest of a +++ header names,
// without a timestamp after a tab, without the quotes and escapes of a
// quoted name, and without a b/ prefix. It returns "" for /dev/null, the
// new version of a deleted file.
func newPath(rest string) string {
	name, _, _ := strings.Cut(rest, "\t")
	if unquoted, err := strconv.Unquote(name); err == nil {
		name = unquoted
	}
	if name == deleted {
		return ""
	}
	// Abs fails only without a working directory, in which the run cannot
	// load its package either.
	path, _ := filepath.Abs(filepath.FromSlash(strings.TrimPrefix(name, newPrefix)))
	return path
}

// hunkHeader returns the old count, the new start and the new count of a
// hunk header @@ -start,count +start,count @@, where a count without its
// comma is 1.
func hunkHeader(line string) (oldCount, start, newCount int, err error) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[3] != hunkEnd || !strings.HasPrefix(fields[1], string(removedLine)) ||
		!strings.HasPrefix(fields[2], string(addedLine)) {
		return 0, 0, 0, fmt.Errorf("selection: the hunk header %q does not parse", line)
	}
	_, oldCount, oldOK := hunkRange(fields[1][1:])
	start, newCount, newOK := hunkRange(fields[2][1:])
	if !oldOK || !newOK {
		return 0, 0, 0, fmt.Errorf("selection: the hunk header %q does not parse", line)
	}
	return oldCount, start, newCount, nil
}

// hunkRange parses start,count, or start alone with a count of 1, and
// reports whether both are numbers of at least 0, a range of at least one
// line starts at line 1 or later, and the number of the line after the
// range's last line, or after the start of an empty range, fits an int.
// Only an empty range starts at 0.
func hunkRange(r string) (start, count int, ok bool) {
	first, rest, comma := strings.Cut(r, ",")
	start, err := strconv.Atoi(first)
	count = 1
	if comma && err == nil {
		count, err = strconv.Atoi(rest)
	}
	return start, count, err == nil && count >= 0 && (start >= 1 || start == 0 && count == 0) &&
		start <= math.MaxInt-max(count, 1)
}
