package fixture

import (
	"os"
	"testing"
)

// TestDouble checks double only in an ordinary build, as a test of a
// property of the build does: it skips while the instrumented program runs.
func TestDouble(t *testing.T) {
	if os.Getenv("DOKIMI_MUTATE_INSTRUMENTED") != "" {
		t.Skip("checks a property of the ordinary build")
	}
	if got := double(3); got != 6 {
		t.Errorf("double(3) = %d, want 6", got)
	}
}

// TestCalls calls double without checking the result, and checks that half
// of 4 is not 0.
func TestCalls(t *testing.T) {
	double(3)
	if half(4) == 0 {
		t.Error("half(4) = 0")
	}
}

// TestTriple checks triple only in an ordinary build, as TestDouble checks
// double. No other test calls triple, so the instrumented program never
// executes it.
func TestTriple(t *testing.T) {
	if os.Getenv("DOKIMI_MUTATE_INSTRUMENTED") != "" {
		t.Skip("checks a property of the ordinary build")
	}
	if got := triple(2); got != 6 {
		t.Errorf("triple(2) = %d, want 6", got)
	}
}
