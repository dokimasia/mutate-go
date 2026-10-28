package fixture

import "testing"

func TestFresh(t *testing.T) {
	if got := fresh(1); got != 2 {
		t.Errorf("fresh(1) = %d, want 2", got)
	}
}
