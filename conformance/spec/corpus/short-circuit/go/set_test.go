package fixture

import "testing"

// TestSet checks a nil option and an option that is on, and no option that
// is off.
func TestSet(t *testing.T) {
	if set(nil) {
		t.Fatal("set(nil) = true")
	}
	if !set(&option{on: true}) {
		t.Fatal("set(on) = false")
	}
}
