package fixture

// sign returns -1 for a negative x, and 1 for any other.
func sign(x int) int {
	if x < 0 {
		return -1
	}
	return 1
}
