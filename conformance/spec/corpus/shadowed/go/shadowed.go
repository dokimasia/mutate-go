package fixture

// point is a point in the plane.
type point struct {
	x, y int
}

// positive reports whether n is positive. A local variable named false
// contains the result.
func positive(n int) bool {
	false := n > 0
	return false
}

// address returns the address of a copy of n. A local variable named nil
// contains it.
func address(n int) *int {
	nil := &n
	return nil
}

// moved returns p one step to the right. A local variable named point, the
// name of its type, contains it.
func moved(p point) point {
	point := point{p.x + 1, p.y}
	return point
}

// first returns the first element of xs. A local variable named new
// contains it.
func first[T any](xs []T) T {
	new := xs[0]
	return new
}

// count returns the number of positive elements of xs in its named result
// n, which the loop writes before the return.
func count(xs []int) (n int, err error) {
	for _, x := range xs {
		if x > 0 {
			n++
		}
	}
	return n, err
}

// half returns half of n in a result named _, and whether n is even.
func half(n int) (_ int, even bool) {
	return n / 2, n%2 == 0
}

// same reports whether a equals b. Local variables named true and false
// contain each other's value.
func same(a, b int) bool {
	true, false := 0 != 0, 0 == 0
	if a == b {
		return false
	}
	return true
}

// _mutateZero0 is the step that next adds. Its name is the name of the first
// variable in the overlay's example of a zero return.
var _mutateZero0 = 1

// next returns n plus the package-level variable _mutateZero0.
func next(n int) int {
	return n + _mutateZero0
}

// double returns twice n in a result named _mutateZero0.
func double(n int) (_mutateZero0 int) {
	return 2 * n
}

// answer returns 42 for a positive n, from a local variable named
// _mutateZero0 that its branch declares, and n otherwise.
func answer(n int) int {
	if n > 0 {
		_mutateZero0 := 42
		return _mutateZero0
	}
	return n
}
