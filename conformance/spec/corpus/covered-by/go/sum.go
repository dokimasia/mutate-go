package fixture

// sum returns a plus b.
func sum(a, b int) int {
	return a + b
}

// double returns n times two, as the sum of n and n.
func double(n int) int {
	return sum(n, n)
}

// half returns n divided by two.
func half(n int) int {
	return n / 2
}
