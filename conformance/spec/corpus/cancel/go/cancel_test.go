package fixture

import "testing"

func TestBounded(t *testing.T) {
	if bounded() == nil {
		t.Fatal("bounded() = nil")
	}
}

func TestLater(t *testing.T) {
	if _, ok := later(true).Deadline(); !ok {
		t.Error("later(true) has no deadline")
	}
	if _, ok := later(false).Deadline(); ok {
		t.Error("later(false) has a deadline")
	}
}

func TestStopped(t *testing.T) {
	if stopped().Err() == nil {
		t.Error("stopped() is not cancelled")
	}
}

func TestEnded(t *testing.T) {
	if _, ok := ended(true).Deadline(); !ok {
		t.Error("ended(true) has no deadline")
	}
	if ended(false).Err() == nil {
		t.Error("ended(false) is not cancelled")
	}
}
