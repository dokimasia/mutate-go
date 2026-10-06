package fixture

import "testing"

func TestReport(t *testing.T) {
	if got := report(3); got != 6 {
		t.Fatalf("report(3) = %d, want 6", got)
	}
}

func TestRoom(t *testing.T) {
	if xs := room(2); xs == nil || len(xs) != 0 {
		t.Fatalf("room(2) = %#v, want an empty list", xs)
	}
}

func TestBuffer(t *testing.T) {
	if b := buffer(4); b == nil || b.Len() != 0 || b.Cap() < 5 {
		t.Fatalf("buffer(4) = %v, want an empty buffer with room for 5 bytes", b)
	}
}
