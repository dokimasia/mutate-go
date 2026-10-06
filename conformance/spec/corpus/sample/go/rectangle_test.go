package fixture

import "testing"

func TestRectangle(t *testing.T) {
	if got := area(2, 3); got != 6 {
		t.Fatalf("area(2, 3) = %d, want 6", got)
	}
	if got := perimeter(2, 3); got != 10 {
		t.Fatalf("perimeter(2, 3) = %d, want 10", got)
	}
}
