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
