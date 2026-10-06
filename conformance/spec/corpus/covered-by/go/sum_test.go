package fixture

import "testing"

func TestDouble(t *testing.T) {
	if got := double(3); got != 6 {
		t.Errorf("double(3) = %d, want 6", got)
	}
}

func TestHalf(t *testing.T) {
	if got := half(6); got != 3 {
		t.Errorf("half(6) = %d, want 3", got)
	}
}

func TestSum(t *testing.T) {
	if got := sum(2, 3); got != 5 {
		t.Errorf("sum(2, 3) = %d, want 5", got)
	}
}
