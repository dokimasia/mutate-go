package fixture

import "testing"

func TestDouble(t *testing.T) {
	if got := double(2); got != 4 {
		t.Fatalf("double(2) = %d, want 4", got)
	}
}
