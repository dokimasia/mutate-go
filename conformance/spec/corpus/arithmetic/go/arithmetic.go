package fixture

// ops applies each arithmetic operator to a and b.
func ops(a, b int) []int {
	return []int{a + b, a - b, a * b, a / b, a % b}
}

// step adds by to n and doubles the sum.
func step(n, by int) int {
	n += by
	n *= 2
	return n
}
