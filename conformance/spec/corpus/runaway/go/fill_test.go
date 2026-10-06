package fixture

import (
	"os"
	"testing"
	"time"
)

// TestFill sleeps half a second in the control runs, where the protocol's
// variable is 0, so the deadline of a mutant's run leaves the mutants that
// allocate without end the time to cross the memory ceiling first.
func TestFill(t *testing.T) {
	if os.Getenv("DOKIMI_MUTATE_MUTANT") == "0" {
		time.Sleep(500 * time.Millisecond)
	}
	if got := len(fill(3)); got != 3 {
		t.Fatalf("len(fill(3)) = %d, want 3", got)
	}
}
