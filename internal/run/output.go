// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"bufio"
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

// marker is the byte that starts each line in which the testing package
// states a test's progress, when the test binary runs with
// -test.v=test2json.
const marker = "\x16"

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

// Scan reads to its end the output of a test binary that runs with
// -test.v=test2json. It reads the progress of the tests from the lines
// that start with marker, and ignores the same text in a line that a test
// writes. It cuts a line at 4,096 bytes, so an output of any size takes
// bounded memory. When failed is not nil, Scan calls it at the first line
// that states a failed test.
func Scan(r io.Reader, failed func()) Output {
	var out Output
	var tail []byte
	var started []string
	ended, paused := map[string]bool{}, map[string]bool{}
	reader := bufio.NewReaderSize(r, maxLine)
	for {
		line, err := readLine(reader)
		if line != "" || err == nil {
			progress, framed := strings.CutPrefix(line, marker)
			switch {
			case !framed:
				out.TimedOut = out.TimedOut || strings.HasPrefix(line, "panic: test timed out after ")
				tail = keep(tail, line)
			case strings.HasPrefix(progress, "=== RUN "):
				name := stateName(progress, "=== RUN ")
				started = append(started, name)
				if !strings.Contains(name, "/") {
					out.Tests = append(out.Tests, name)
				}
			case strings.HasPrefix(progress, "=== PAUSE "):
				paused[stateName(progress, "=== PAUSE ")] = true
			case strings.HasPrefix(progress, "=== CONT "):
				paused[stateName(progress, "=== CONT ")] = false
			case strings.HasPrefix(progress, "--- FAIL: "):
				name := testName(progress)
				ended[name] = true
				if len(out.Failed) == 0 && failed != nil {
					failed()
				}
				out.Failed = appendOnce(out.Failed, name)
				tail = keep(tail, progress)
			case strings.HasPrefix(progress, "--- PASS: "), strings.HasPrefix(progress, "--- SKIP: "):
				ended[testName(progress)] = true
			}
		}
		if err != nil {
			break
		}
	}
	// A subtest's name is its parent's, a slash and its own, so each prefix
	// of a name before a slash is an ancestor of the subtest. An ancestor of
	// a subtest in progress is a parent, and an ancestor of a subtest that
	// runs waits.
	parents, waits := map[string]bool{}, map[string]bool{}
	for _, name := range started {
		if ended[name] {
			continue
		}
		for i := strings.LastIndexByte(name, '/'); i > 0; i = strings.LastIndexByte(name[:i], '/') {
			parents[name[:i]] = true
			waits[name[:i]] = waits[name[:i]] || !paused[name]
		}
	}
	var parentsOfPaused []string
	for _, name := range started {
		switch {
		case ended[name] || paused[name] || waits[name]:
		case parents[name]:
			parentsOfPaused = append(parentsOfPaused, name)
		default:
			out.Running = append(out.Running, name)
		}
	}
	if len(out.Running) == 0 {
		out.Running = parentsOfPaused
	}
	if len(tail) > maxTail {
		tail = tail[len(tail)-maxTail:]
	}
	out.Tail = string(tail)
	return out
}

// keep adds line to the end of the output that Scan keeps, and drops the
// start of it once it is twice as long as Scan keeps.
func keep(tail []byte, line string) []byte {
	tail = append(tail, line...)
	tail = append(tail, '\n')
	if len(tail) > 2*maxTail {
		tail = append(tail[:0], tail[len(tail)-maxTail:]...)
	}
	return tail
}

// stateName returns the name of the test that a line prefix name states.
func stateName(progress, prefix string) string {
	return strings.TrimSpace(strings.TrimPrefix(progress, prefix))
}

// testName returns the name of the test that a line --- STATUS: name (time)
// states the end of.
func testName(progress string) string {
	name, _, _ := strings.Cut(progress[len("--- FAIL: "):], " ")
	return name
}

// readLine returns the next line without its line break, cut at maxLine
// bytes. The rest of a longer line is read and dropped.
func readLine(r *bufio.Reader) (string, error) {
	data, isPrefix, err := r.ReadLine()
	line := string(data)
	for isPrefix && err == nil {
		_, isPrefix, err = r.ReadLine()
	}
	return line, err
}

func appendOnce(list []string, name string) []string {
	if slices.Contains(list, name) {
		return list
	}
	return append(list, name)
}
