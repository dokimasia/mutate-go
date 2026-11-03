package fixture

import "testing"

// TestPair checks each function in a subtest of its own, with 3 and 2.
func TestPair(t *testing.T) {
	t.Run("larger", func(t *testing.T) {
		if got := larger(3, 2); got != 3 {
			t.Errorf("larger(3, 2) = %d, want 3", got)
		}
	})
	t.Run("smaller", func(t *testing.T) {
		if got := smaller(3, 2); got != 2 {
			t.Errorf("smaller(3, 2) = %d, want 2", got)
		}
	})
}

// TestEqual checks larger with two equal numbers, so it returns b.
func TestEqual(t *testing.T) {
	if got := larger(2, 2); got != 2 {
		t.Errorf("larger(2, 2) = %d, want 2", got)
	}
}
