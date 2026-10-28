package fixture

import "testing"

func TestRecent(t *testing.T) {
	if got := recent(1); got != 5 {
		t.Errorf("recent(1) = %d, want 5", got)
	}
}
