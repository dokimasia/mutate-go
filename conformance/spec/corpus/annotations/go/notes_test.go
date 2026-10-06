package fixture

import "testing"

func TestNotes(t *testing.T) {
	if clamp(12) != 10 || clamp(3) != 3 || half(4) != 2 || twice(4) != 8 {
		t.Fatal("a function returns a wrong value")
	}
}
