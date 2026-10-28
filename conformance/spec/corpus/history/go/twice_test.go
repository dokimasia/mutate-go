package fixture

import "testing"

func TestTwice(t *testing.T) {
	if got := twice(3); got != 6 {
		t.Errorf("twice(3) = %d, want 6", got)
	}
}

func TestHalf(t *testing.T) {
	half(6)
}
