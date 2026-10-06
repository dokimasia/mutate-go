package fixture

import "testing"

// TestInc expects a wrong value, so it fails with no mutant active.
func TestInc(t *testing.T) {
	if got := inc(1); got != 3 {
		t.Fatalf("inc(1) = %d, want 3", got)
	}
}
