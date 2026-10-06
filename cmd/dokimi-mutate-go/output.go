// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/report"
)

// workingDir is the command's working directory, against which the output
// states the path of each mutant's file.
const workingDir = "."

// output writes the command's results. To stdout it writes the line of each
// mutant that a report lists, when the mutant's verdict is final, and the
// summary or the record of each package, or the listing of each package.
// To stderr it writes the notes on each run and the command's errors, each
// after the command's name.
//
// # Concurrency
//
// An output is safe for concurrent use when stdout and stderr are. Each
// call writes each of its texts with one write, so the lines of two
// packages do not interleave.
type output struct {
	stdout, stderr io.Writer
	// json writes each package's record in place of the text.
	json bool
}

// line writes the line of m, whose file is at path, when a report lists m
// and the output is text.
func (o *output) line(m record.Mutant, path string) {
	if !o.json && report.Listed(m.Verdict) {
		_, _ = io.WriteString(o.stdout, report.Line(m, path)+"\n")
	}
}

// result writes the end of one package's run: the notes on the run, and
// the record as one line of JSON or the summary.
func (o *output) result(rec *record.Record) {
	o.notes(rec)
	if o.json {
		// A record contains strings, integers and finite numbers, which
		// encoding/json encodes without an error.
		data, _ := json.Marshal(rec)
		_, _ = o.stdout.Write(append(data, '\n'))
		return
	}
	_, _ = io.WriteString(o.stdout, report.Summary(rec, definition.Protocol)+"\n")
}

// listing writes the listing of one package: the notes on the listing, and
// the line of each mutant and the count of the mutants.
func (o *output) listing(rec *record.Record) {
	o.notes(rec)
	var b strings.Builder
	for _, m := range rec.Mutants {
		b.WriteString(report.Planned(m, rec.Path(m.File, workingDir)) + "\n")
	}
	b.WriteString(report.Plan(rec, definition.Protocol) + "\n")
	_, _ = io.WriteString(o.stdout, b.String())
}

// notes writes each note on rec to stderr, after the package's import path.
func (o *output) notes(rec *record.Record) {
	for _, note := range report.Notes(rec) {
		o.errorf("%s: %s", rec.Target.Name, note)
	}
}

// errorf writes the message of format and args to stderr, as one line after
// the command's name.
func (o *output) errorf(format string, args ...any) {
	_, _ = io.WriteString(o.stderr, name+": "+fmt.Sprintf(format, args...)+"\n")
}

// lockedWriter serializes the writes to w, so the output and the progress
// can write to one standard error from several goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// Write writes data to w under the lock.
func (l *lockedWriter) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(data)
}
