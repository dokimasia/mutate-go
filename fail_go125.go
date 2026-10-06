// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build go1.25

package mutate

import (
	"fmt"
	"testing"
)

// fail writes line to the test's output without the source position that
// go test puts before a logged line, and marks the test failed. An editor
// then links the position at the start of line.
func fail(tb testing.TB, line string) {
	tb.Helper()
	fmt.Fprintln(tb.Output(), line)
	tb.Fail()
}
