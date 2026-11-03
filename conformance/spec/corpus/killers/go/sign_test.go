package fixture

import "testing"

// TestSign checks sign in four subtests. zero and positive both detect a
// sign of 0 for a number that is not negative. wait blocks until the
// deadline when sign gives -3 another sign than -1, so a run that runs every
// subtest after the failure of negative ends at the deadline.
func TestSign(t *testing.T) {
	t.Run("negative", func(t *testing.T) {
		if got := sign(-2); got != -1 {
			t.Errorf("sign(-2) = %d, want -1", got)
		}
	})
	t.Run("zero", func(t *testing.T) {
		if got := sign(0); got != 1 {
			t.Errorf("sign(0) = %d, want 1", got)
		}
	})
	t.Run("positive", func(t *testing.T) {
		if got := sign(2); got != 1 {
			t.Errorf("sign(2) = %d, want 1", got)
		}
	})
	t.Run("wait", func(t *testing.T) {
		if sign(-3) != -1 {
			select {}
		}
	})
}
