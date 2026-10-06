package fixture

import "testing"

// TestBetween checks lo itself and no value equal to hi.
func TestBetween(t *testing.T) {
	cases := []struct {
		x    int
		want bool
	}{{0, false}, {1, true}, {2, true}, {5, false}}
	for _, c := range cases {
		if got := between(c.x, 1, 3); got != c.want {
			t.Fatalf("between(%d, 1, 3) = %v, want %v", c.x, got, c.want)
		}
	}
}

// TestLeast checks lo itself and a value below it.
func TestLeast(t *testing.T) {
	if !least(1, 1) || least(0, 1) {
		t.Fatal("least does not accept exactly the values from lo")
	}
}
