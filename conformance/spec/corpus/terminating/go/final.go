package fixture

// sum is a running total.
type sum struct {
	total int
}

// prefix adds the values of xs before the first 0 to the total.
func (s *sum) prefix(xs []int) {
	i := 0
	for {
		if xs[i] == 0 {
			break
		}
		s.total += xs[i]
		i++
	}
}

// grade sets *g to the grade of a score of 50 or more, and leaves *g
// unchanged for a lower score.
func grade(score int, g *string) {
	switch {
	case score >= 80:
		*g = "high"
		return
	case score >= 50:
		*g = "pass"
		return
	}
}
