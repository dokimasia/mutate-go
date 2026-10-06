// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package testbin

import (
	"bufio"
	"bytes"
	"io"
	"slices"
	"strings"
)

// The limits of what Scan keeps of a test binary's output.
const (
	// maxLine is the length at which Scan cuts a line.
	maxLine = 4096
	// maxTail is the length of the end of the output that Scan keeps.
	maxTail = 64 << 10
)

// The text that Scan matches in a test binary's output.
var (
	// marker starts each line in which the testing package states a test's
	// progress, when the binary runs with -test.v=test2json.
	marker = []byte("\x16")
	// The progress lines, after the marker, each followed by the test's
	// name.
	runLine   = []byte("=== RUN ")
	pauseLine = []byte("=== PAUSE ")
	contLine  = []byte("=== CONT ")
	failLine  = []byte("--- FAIL: ")
	passLine  = []byte("--- PASS: ")
	skipLine  = []byte("--- SKIP: ")
	// timedOut starts the line of the panic that the testing package's alarm
	// raises.
	timedOut = []byte("panic: test timed out after ")
)

// Output is what a test binary's output states about its tests.
type Output struct {
	// Failed lists the tests that a FAIL line names, in output order.
	Failed []string
	// Running lists the tests that ran code when the run ended, in the order
	// in which they started. A test that started and did not end runs code
	// unless it paused as a parallel test and did not continue, or a subtest
	// of it that started and did not end runs. A test runs none of its own
	// code while it waits in t.Run for a subtest, or for its parallel
	// subtests after its function returned. A test whose subtests in progress
	// all paused can still run its own code, so Running lists such tests when
	// no other test runs code.
	Running []string
	// Tests lists the top-level tests that started, in the order in which
	// they started.
	Tests []string
	// TimedOut reports whether the testing package's alarm ended the run.
	TimedOut bool
	// Tail is the end of the output, at most 64 KiB. It contains the lines
	// that the tests and the runtime write, and the FAIL lines, and leaves
	// out the lines that state that a test started, paused, continued,
	// passed or was skipped.
	Tail string
}

// test is the state of one test that an output states.
type test struct {
	name string
	// started reports a test that a RUN line states, and ended one that a
	// PASS, FAIL or SKIP line states.
	started, ended bool
	paused         bool
	// parent reports a test with a subtest in progress, and waits one with
	// a subtest in progress that did not pause.
	parent, waits bool
}

// scanner is the state of one Scan.
type scanner struct {
	// tests lists every test that the output names, in the order that it
	// names them, and index maps each name to its position.
	tests []test
	index map[string]int
}

// Scan reads to its end the output of a test binary that runs with
// -test.v=test2json. It reads the progress of the tests from the lines
// that start with the marker, and ignores the same text in a line that a
// test writes. It cuts a line at 4,096 bytes, so an output of any size takes
// bounded memory. When failed is not nil, Scan calls it at the first line
// that states a failed test.
//
// # Allocation contract
//
// Scan allocates its reader, the buffers of the line and of the tail, which
// stop growing at 4 KiB and 128 KiB, the name of each test that the output
// names, the growth of its lists of tests, and the text of the tail. It
// allocates nothing for a line beyond those, so its count grows with the
// number of tests and not with the number of lines: 49 allocations for an
// output of 20 passing tests that each write one line.
func Scan(r io.Reader, failed func()) Output {
	var out Output
	var line, tail []byte
	s := &scanner{index: map[string]int{}}
	reader := bufio.NewReaderSize(r, maxLine)
	for {
		var err error
		line, err = readLine(reader, line)
		if len(line) > 0 || err == nil {
			progress, framed := bytes.CutPrefix(line, marker)
			switch {
			case !framed:
				out.TimedOut = out.TimedOut || bytes.HasPrefix(line, timedOut)
				tail = keep(tail, line)
			case bytes.HasPrefix(progress, runLine):
				t := s.lookup(bytes.TrimSpace(progress[len(runLine):]))
				if !s.tests[t].started {
					s.tests[t].started = true
					if strings.IndexByte(s.tests[t].name, '/') < 0 {
						out.Tests = append(out.Tests, s.tests[t].name)
					}
				}
			case bytes.HasPrefix(progress, pauseLine):
				s.tests[s.lookup(bytes.TrimSpace(progress[len(pauseLine):]))].paused = true
			case bytes.HasPrefix(progress, contLine):
				s.tests[s.lookup(bytes.TrimSpace(progress[len(contLine):]))].paused = false
			case bytes.HasPrefix(progress, failLine):
				t := s.lookup(ended(progress))
				s.tests[t].ended = true
				if len(out.Failed) == 0 && failed != nil {
					failed()
				}
				if !slices.Contains(out.Failed, s.tests[t].name) {
					out.Failed = append(out.Failed, s.tests[t].name)
				}
				tail = keep(tail, progress)
			case bytes.HasPrefix(progress, passLine), bytes.HasPrefix(progress, skipLine):
				s.tests[s.lookup(ended(progress))].ended = true
			}
		}
		if err != nil {
			break
		}
	}
	out.Running = s.running()
	if len(tail) > maxTail {
		tail = tail[len(tail)-maxTail:]
	}
	out.Tail = string(tail)
	return out
}

// lookup returns the position of the test name, and adds the test when the
// output did not name it before.
func (s *scanner) lookup(name []byte) int {
	if i, ok := s.index[string(name)]; ok {
		return i
	}
	s.tests = append(s.tests, test{name: string(name)})
	s.index[s.tests[len(s.tests)-1].name] = len(s.tests) - 1
	return len(s.tests) - 1
}

// running returns the tests that started, in their order, that ran code
// when the output ended, as Output.Running states them. A subtest's name is
// its parent's, a slash and its own, so each prefix of a name before a slash
// names an ancestor of the subtest. An ancestor of a subtest in progress is
// a parent, and an ancestor of a subtest that runs waits for it.
func (s *scanner) running() []string {
	for _, t := range s.tests {
		if !t.started || t.ended {
			continue
		}
		for i := strings.LastIndexByte(t.name, '/'); i > 0; i = strings.LastIndexByte(t.name[:i], '/') {
			if a, ok := s.index[t.name[:i]]; ok {
				s.tests[a].parent = true
				s.tests[a].waits = s.tests[a].waits || !t.paused
			}
		}
	}
	var runs, parentsOfPaused []string
	for _, t := range s.tests {
		switch {
		case !t.started || t.ended || t.paused || t.waits:
		case t.parent:
			parentsOfPaused = append(parentsOfPaused, t.name)
		default:
			runs = append(runs, t.name)
		}
	}
	if len(runs) == 0 {
		return parentsOfPaused
	}
	return runs
}

// ended returns the name of the test that a line --- STATUS: name (time)
// states the end of.
func ended(progress []byte) []byte {
	name := progress[len(failLine):]
	if i := bytes.IndexByte(name, ' '); i >= 0 {
		name = name[:i]
	}
	return name
}

// keep adds line to the end of the output that Scan keeps, and drops the
// start of it once it is twice as long as Scan keeps.
func keep(tail, line []byte) []byte {
	tail = append(tail, line...)
	tail = append(tail, '\n')
	if len(tail) > 2*maxTail {
		tail = append(tail[:0], tail[len(tail)-maxTail:]...)
	}
	return tail
}

// readLine reads the next line into buf, without its line break and cut at
// maxLine bytes, and returns buf. The rest of a longer line is read and
// dropped.
func readLine(r *bufio.Reader, buf []byte) ([]byte, error) {
	data, isPrefix, err := r.ReadLine()
	buf = append(buf[:0], data...)
	for isPrefix && err == nil {
		_, isPrefix, err = r.ReadLine()
	}
	return buf, err
}
