package fixture

import "testing"

func TestKeep(t *testing.T) {
	if keep("bb") != 2 || keep("a") != 1 {
		t.Fatal("keep returns a wrong length")
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "bb" {
		t.Fatalf("names = %q, want [a bb]", names)
	}
}

func TestSmall(t *testing.T) {
	m := map[string]int{"one": 1, "ten": 10, "big": 20}
	if !small(m, "one") || small(m, "ten") || small(m, "big") || small(m, "none") {
		t.Fatal("small does not report exactly the values below 10")
	}
}

func TestSize(t *testing.T) {
	if got := size("abc"); got != 3 {
		t.Fatalf("size(abc) = %d, want 3", got)
	}
}

func TestUpper(t *testing.T) {
	if got := upper("ab"); got != "AB" {
		t.Fatalf("upper(ab) = %q, want AB", got)
	}
}

func TestLength(t *testing.T) {
	if length("ab") != 2 || length([]int{1}) != 1 || length(3) != -1 {
		t.Fatal("length does not return the length of each value")
	}
}

func TestFirst(t *testing.T) {
	for _, c := range []struct {
		xs   []int
		want int
	}{{[]int{3, -1, 2}, 1}, {[]int{0, -2}, 1}, {[]int{1}, 1}} {
		if got := first(c.xs); got != c.want {
			t.Fatalf("first(%v) = %d, want %d", c.xs, got, c.want)
		}
	}
}
