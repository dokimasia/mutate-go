package fixture

import "testing"

func TestSteps(t *testing.T) {
	for n, want := range map[int]int{0: 0, 1: 0, 3: 1} {
		if got := steps(n); got != want {
			t.Fatalf("steps(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestCounter(t *testing.T) {
	c := &counter{n: 2}
	if c.add() != 3 || c.sub() != 1 || c.twice() != 4 {
		t.Fatal("a method of counter returns a wrong value")
	}
}
