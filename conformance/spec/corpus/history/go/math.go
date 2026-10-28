package fixture

// dec returns n minus one.
func dec(n int) int {
	return n - 1
}

// twice returns n times two.
func twice(n int) int {
	return n * 2
}

// half returns n divided by two.
func half(n int) int {
	return n / 2
}

// unused returns n plus three, and no test calls it.
func unused(n int) int {
	return n + 3
}
