package fixture

import "testing"

// tester is the part of testing.TB that same calls, as a library declares
// it.
type tester interface {
	Helper()
	Errorf(format string, args ...any)
}

// marker declares a method Helper with a parameter, which marks no test
// helper.
type marker interface {
	Helper(depth int)
}

// equal reports a failure on t when got differs from want.
func equal(t testing.TB, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

// same reports a failure on t when got differs from want.
func same(t tester, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

// mark calls the method Helper of m with the depth 1.
func mark(m marker) {
	m.Helper(1)
}
