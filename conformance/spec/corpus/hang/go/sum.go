package fixture

// sum returns the sum of the numbers from 1 to n.
func sum(n int) int {
	total := 0
	for i := 1; i <= n; i++ {
		total += i
	}
	return total
}
