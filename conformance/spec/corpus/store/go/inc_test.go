package fixture

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestInc stores a wrong result in testdata/failure when it fails, as a
// library stores a failing input for later runs to replay.
func TestInc(t *testing.T) {
	if got := inc(1); got != 2 {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("testdata", "failure"), []byte(strconv.Itoa(got)), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("inc(1) = %d, want 2", got)
	}
}
