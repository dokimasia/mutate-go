package fixture

// between reports whether x lies in the range from lo to hi, with lo in the
// range and hi outside it.
func between(x, lo, hi int) bool {
	if x < lo {
		return false
	}
	return x < hi
}

// least reports whether x is at least lo.
func least(x, lo int) bool {
	return x >= lo
}
