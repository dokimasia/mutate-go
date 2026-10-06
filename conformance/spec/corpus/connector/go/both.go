package fixture

// both reports whether a and b are both true.
func both(a, b bool) bool {
	return a && b
}

// either reports whether a or b is true.
func either(a, b bool) bool {
	return a || b
}
