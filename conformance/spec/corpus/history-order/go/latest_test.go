package fixture

import "testing"

func TestLatest(t *testing.T) {
	if got := latest(1); got != 6 {
		t.Errorf("latest(1) = %d, want 6", got)
	}
}
