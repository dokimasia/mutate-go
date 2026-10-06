package fixture

import (
	"os"
	"testing"
)

// TestInc passes only while the file marker is absent from the package
// directory, and creates it.
func TestInc(t *testing.T) {
	if _, err := os.Stat("marker"); err == nil {
		t.Fatal("an earlier run left the file marker")
	}
	if err := os.WriteFile("marker", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := inc(1); got != 2 {
		t.Fatalf("inc(1) = %d, want 2", got)
	}
}
