package fixture

import "testing"

// TestScaleByOne checks only the factor 1, under which x times factor and x
// divided by factor are equal.
func TestScaleByOne(t *testing.T) {
	if got := Scale(2, 1); got != 2 {
		t.Fatalf("Scale(2, 1) = %d, want 2", got)
	}
}
