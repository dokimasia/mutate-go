package fixture

import "testing"

func TestInc(t *testing.T) {
	if got := inc(2); got != 3 {
		t.Errorf("inc(2) = %d, want 3", got)
	}
}
