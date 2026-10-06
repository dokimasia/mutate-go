// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build !go1.25

package mutate

import "testing"

// fail reports line as a failure of the test. go test puts the position of
// the call to Check before it.
func fail(tb testing.TB, line string) {
	tb.Helper()
	tb.Error(line)
}
