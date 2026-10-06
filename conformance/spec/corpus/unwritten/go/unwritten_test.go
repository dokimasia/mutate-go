package fixture

import "testing"

func TestIndex(t *testing.T) {
	s := []string{"a"}
	if v, ok := index(s, 0); !ok || v != "a" {
		t.Fatalf("index(s, 0) = %q, %v", v, ok)
	}
	if v, ok := index(s, 1); ok || v != "" {
		t.Fatalf("index(s, 1) = %q, %v", v, ok)
	}
}

func TestNone(t *testing.T) {
	if err := none(); err != nil {
		t.Fatalf("none() = %v", err)
	}
}

func TestAssigned(t *testing.T) {
	if got := assigned(); got != 3 {
		t.Fatalf("assigned() = %d, want 3", got)
	}
}

func TestField(t *testing.T) {
	if got := field(); got != (point{x: 2}) {
		t.Fatalf("field() = %v, want {2 0}", got)
	}
}

func TestElement(t *testing.T) {
	if got := element(); got != [2]int{4, 0} {
		t.Fatalf("element() = %v, want [4 0]", got)
	}
}

func TestDeclared(t *testing.T) {
	if n, err := declared("7"); n != 7 || err != nil {
		t.Fatalf("declared(7) = %d, %v", n, err)
	}
}

func TestIncremented(t *testing.T) {
	if got := incremented(); got != 1 {
		t.Fatalf("incremented() = %d, want 1", got)
	}
}

func TestLast(t *testing.T) {
	if got := last([]int{1, 2}); got != 2 {
		t.Fatalf("last(1, 2) = %d, want 2", got)
	}
}

func TestAddressed(t *testing.T) {
	if got := addressed(); got != 5 {
		t.Fatalf("addressed() = %d, want 5", got)
	}
}

func TestSliced(t *testing.T) {
	if got := sliced(); got != [2]byte{'a', 'b'} {
		t.Fatalf("sliced() = %q, want ab", got)
	}
}

func TestShifted(t *testing.T) {
	if got := shifted(); got != (point{x: 1}) {
		t.Fatalf("shifted() = %v, want {1 0}", got)
	}
}

func TestEnclosed(t *testing.T) {
	if got := enclosed(); got != 8 {
		t.Fatalf("enclosed() = %d, want 8", got)
	}
}

func TestLater(t *testing.T) {
	if got := later([]int{6, 7}); got != 6 {
		t.Fatalf("later(6, 7) = %d, want 6", got)
	}
	if got := later([]int{6}); got != -1 {
		t.Fatalf("later(6) = %d, want -1", got)
	}
}
