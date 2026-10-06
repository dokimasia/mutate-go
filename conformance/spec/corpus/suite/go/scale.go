// Package fixture scales numbers. The tests of the package conformance
// exercise it as well.
package fixture

// Scale returns x times factor.
func Scale(x, factor int) int {
	return x * factor
}
