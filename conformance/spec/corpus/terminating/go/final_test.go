package fixture

import "testing"

func TestPrefix(t *testing.T) {
	var s sum
	s.prefix([]int{1, 2, 0, 4})
	if s.total != 3 {
		t.Fatalf("prefix(1, 2, 0, 4) added %d, want 3", s.total)
	}
}

func TestGrade(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{score: 80, want: "high"},
		{score: 60, want: "pass"},
		{score: 50, want: "pass"},
		{score: 10, want: ""},
	}
	for _, tt := range tests {
		g := ""
		grade(tt.score, &g)
		if g != tt.want {
			t.Fatalf("grade(%d) = %q, want %q", tt.score, g, tt.want)
		}
	}
}
