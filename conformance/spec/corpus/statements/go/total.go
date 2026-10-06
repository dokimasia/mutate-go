package fixture

// last is the sum of the latest call to total.
var last int

// total returns the sum of xs, and stores it in last.
func total(xs []int) int {
	sum := 0
	for _, x := range xs {
		sum += x
	}
	last = sum
	return sum
}
