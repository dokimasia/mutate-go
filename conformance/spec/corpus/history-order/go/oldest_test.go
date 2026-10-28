package fixture

import "testing"

func TestOldest(t *testing.T) {
	if got := oldest(1); got != 3 {
		t.Errorf("oldest(1) = %d, want 3", got)
	}
}
