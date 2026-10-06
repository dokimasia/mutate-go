package fixture

import "testing"

// TestNegate checks that the result is not zero, and not its sign.
func TestNegate(t *testing.T) {
	if negate(2) == 0 {
		t.Fatal("negate(2) = 0")
	}
}

func TestFlip(t *testing.T) {
	if flip(true) || !flip(false) {
		t.Fatal("flip does not return the opposite")
	}
}

func TestCount(t *testing.T) {
	if got := count(1); got != 2 {
		t.Fatalf("count(1) = %d, want 2", got)
	}
}
