package fixture

// larger returns the larger of a and b.
func larger(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// smaller returns the smaller of a and b.
func smaller(a, b int) int {
	if a < b {
		return a
	}
	return b
}
