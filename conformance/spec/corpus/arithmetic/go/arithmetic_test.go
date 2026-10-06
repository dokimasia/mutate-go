package fixture

import "testing"

// TestOps checks every result but the remainder.
func TestOps(t *testing.T) {
	got := ops(7, 2)
	if len(got) != 5 || got[0] != 9 || got[1] != 5 || got[2] != 14 || got[3] != 3 {
		t.Fatalf("ops(7, 2) = %v", got)
	}
}

func TestStep(t *testing.T) {
	if got := step(3, 1); got != 8 {
		t.Fatalf("step(3, 1) = %d, want 8", got)
	}
}
