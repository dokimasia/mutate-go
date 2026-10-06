package fixture

import "testing"

func TestInside(t *testing.T) {
	if got := inside(1); got != 1 {
		t.Fatalf("inside(1) = %d, want 1", got)
	}
}

func TestPlain(t *testing.T) {
	if got := plain(1); got != 1 {
		t.Fatalf("plain(1) = %d, want 1", got)
	}
}
