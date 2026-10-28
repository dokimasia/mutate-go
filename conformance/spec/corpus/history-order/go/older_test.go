package fixture

import "testing"

func TestOlder(t *testing.T) {
	if got := older(1); got != 4 {
		t.Errorf("older(1) = %d, want 4", got)
	}
}
