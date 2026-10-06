package fixture

import "testing"

func TestFind(t *testing.T) {
	ps := []point{{x: 1, y: 2}, {x: 3, y: 4}}
	if p, ok := find(ps, 3); !ok || p != (point{x: 3, y: 4}) {
		t.Fatalf("find(3) = %v, %v", p, ok)
	}
	if p, ok := find(ps, 5); ok || p != (point{}) {
		t.Fatalf("find(5) = %v, %v", p, ok)
	}
}

func TestOrigin(t *testing.T) {
	if p, pair, err := origin(); p != (point{}) || pair != [2]int{} || err != nil {
		t.Fatalf("origin() = %v, %v, %v", p, pair, err)
	}
}

func TestFirst(t *testing.T) {
	if got := first([]int{7}); got != 7 {
		t.Fatalf("first(7) = %d, want 7", got)
	}
	if got := first[int](nil); got != 0 {
		t.Fatalf("first(nil) = %d, want 0", got)
	}
}

func TestEmpty(t *testing.T) {
	if xs := empty(); xs == nil || len(xs) != 0 {
		t.Fatalf("empty() = %#v, want an empty list", xs)
	}
}

func TestAnswer(t *testing.T) {
	if got := answer(); got != 0 {
		t.Fatalf("answer() = %v, want 0", got)
	}
}

func TestLabelled(t *testing.T) {
	if p := labelled(); p.label != "" {
		t.Fatalf("labelled() = %v, want the empty label", p)
	}
}
