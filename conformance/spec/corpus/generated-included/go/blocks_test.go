package fixture

import "testing"

func TestBlocks(t *testing.T) {
	if got := blocks(2); got != 8192 {
		t.Fatalf("blocks(2) = %d, want 8192", got)
	}
}

func TestNext(t *testing.T) {
	if got := next(1); got != 2 {
		t.Fatalf("next(1) = %d, want 2", got)
	}
}
