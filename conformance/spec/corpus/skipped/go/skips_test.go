package fixture

import "testing"

func TestSum(t *testing.T) {
	if got := sum(2, 3); got != 5 {
		t.Fatalf("sum(2, 3) = %d, want 5", got)
	}
}

func TestPositive(t *testing.T) {
	if !positive(1) {
		t.Fatal("positive(1) = false, want true")
	}
}

func TestBump(t *testing.T) {
	xs := []int{1}
	bump(xs, 2)
	if xs[0] != 3 {
		t.Fatalf("bump(1, 2) left %d, want 3", xs[0])
	}
}

func TestLater(t *testing.T) {
	if !later(4, 1) {
		t.Fatal("later(4, 1) = false, want true")
	}
}
