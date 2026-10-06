package fixture

import "testing"

// TestBoth checks no call in which a is false.
func TestBoth(t *testing.T) {
	if !both(true, true) {
		t.Fatal("both(true, true) = false")
	}
	if both(true, false) {
		t.Fatal("both(true, false) = true")
	}
}

// TestEither checks a call with each operand true alone, and with neither.
func TestEither(t *testing.T) {
	if !either(true, false) {
		t.Fatal("either(true, false) = false")
	}
	if !either(false, true) {
		t.Fatal("either(false, true) = false")
	}
	if either(false, false) {
		t.Fatal("either(false, false) = true")
	}
}
