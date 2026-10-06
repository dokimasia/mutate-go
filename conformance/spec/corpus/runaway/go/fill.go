package fixture

// fill returns a list of n zeros.
func fill(n int) []int {
	var xs []int
	for i := 0; i != n; i++ {
		xs = append(xs, 0)
	}
	return xs
}
