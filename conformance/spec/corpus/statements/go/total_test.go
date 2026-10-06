package fixture

import "testing"

// TestTotal checks the sum, and not last.
func TestTotal(t *testing.T) {
	if got := total([]int{3, 2}); got != 5 {
		t.Fatalf("total(3, 2) = %d, want 5", got)
	}
}
