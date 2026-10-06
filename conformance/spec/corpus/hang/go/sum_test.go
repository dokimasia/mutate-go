package fixture

import "testing"

func TestSum(t *testing.T) {
	if got := sum(3); got != 6 {
		t.Fatalf("sum(3) = %d, want 6", got)
	}
}
