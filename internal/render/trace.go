// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"strconv"
)

// TraceVar is the variable of the environment that names the trace file of
// a run of the instrumented test binary. The binary appends to the file,
// and traces nothing when the variable is unset or empty.
const TraceVar = "DOKIMI_MUTATE_TRACE"

// traceStart is the line that the instrumented package writes into the
// trace when it initializes, before any site executes.
const traceStart = "start"

// ParseTrace reads a trace that the instrumented package wrote. It returns
// the first ordinal of each site that executed, and whether the trace has
// the line that the package writes when it initializes, which shows that
// the test binary ran the instrumented package. It ignores any other line.
func ParseTrace(trace []byte) (executed map[int]bool, started bool) {
	executed = map[int]bool{}
	for line := range bytes.Lines(trace) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if string(line) == traceStart {
			started = true
		} else if o, err := strconv.Atoi(string(line)); err == nil {
			executed[o] = true
		}
	}
	return executed, started
}
