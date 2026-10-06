package fixture

import (
	"os"
	"testing"
)

func TestPositive(t *testing.T) {
	if !positive(1) || positive(0) {
		t.Fatalf("positive(1) = %v, positive(0) = %v", positive(1), positive(0))
	}
}

func TestAddress(t *testing.T) {
	if p := address(1); p == nil || *p != 1 {
		t.Fatalf("address(1) = %v, want the address of 1", p)
	}
}

func TestMoved(t *testing.T) {
	if got := moved(point{x: 1, y: 2}); got != (point{x: 2, y: 2}) {
		t.Fatalf("moved({1 2}) = %v, want {2 2}", got)
	}
}

func TestFirst(t *testing.T) {
	if got := first([]int{7}); got != 7 {
		t.Fatalf("first([7]) = %d, want 7", got)
	}
}

func TestCount(t *testing.T) {
	if n, err := count([]int{1, 0, 2}); n != 2 || err != nil {
		t.Fatalf("count([1 0 2]) = %d, %v, want 2, nil", n, err)
	}
}

func TestHalf(t *testing.T) {
	if n, even := half(4); n != 2 || !even {
		t.Fatalf("half(4) = %d, %v, want 2, true", n, even)
	}
	if n, even := half(3); n != 1 || even {
		t.Fatalf("half(3) = %d, %v, want 1, false", n, even)
	}
}

// TestSame checks same only in an ordinary build: it skips while the
// instrumented program runs, so each mutant of same runs in its ordinary
// build, whose source writes the constants that skip code.
func TestSame(t *testing.T) {
	if os.Getenv("DOKIMI_MUTATE_INSTRUMENTED") != "" {
		t.Skip("checks the ordinary build of each mutant of same")
	}
	if !same(1, 1) || same(1, 2) {
		t.Fatalf("same(1, 1) = %v, same(1, 2) = %v", same(1, 1), same(1, 2))
	}
}
