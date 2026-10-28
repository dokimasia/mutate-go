package fixture

import "testing"

func TestDec(t *testing.T) {
	if got := dec(2); got != 1 {
		t.Errorf("dec(2) = %d, want 1", got)
	}
}
