package conformance

import (
	"testing"

	"fixture"
)

func TestScale(t *testing.T) {
	if got := fixture.Scale(6, 3); got != 18 {
		t.Fatalf("Scale(6, 3) = %d, want 18", got)
	}
}
