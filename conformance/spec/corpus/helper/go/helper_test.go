package fixture

import "testing"

// recorder records whether a check reported a failure.
type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(string, ...any) { r.failed = true }

// depth adds up the depths that Helper receives.
type depth struct {
	sum int
}

func (d *depth) Helper(n int) { d.sum += n }

func TestEqual(t *testing.T) {
	r := &recorder{TB: t}
	equal(r, 1, 2)
	if !r.failed {
		t.Fatal("equal(1, 2) passed")
	}
	r = &recorder{TB: t}
	equal(r, 2, 2)
	if r.failed {
		t.Fatal("equal(2, 2) failed")
	}
}

func TestSame(t *testing.T) {
	r := &recorder{TB: t}
	same(r, 1, 2)
	if !r.failed {
		t.Fatal("same(1, 2) passed")
	}
	r = &recorder{TB: t}
	same(r, 2, 2)
	if r.failed {
		t.Fatal("same(2, 2) failed")
	}
}

func TestMark(t *testing.T) {
	d := &depth{}
	mark(d)
	if d.sum != 1 {
		t.Fatalf("mark passed the depth %d, want 1", d.sum)
	}
}
