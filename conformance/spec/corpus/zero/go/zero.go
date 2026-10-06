package fixture

// point is a point in the plane.
type point struct {
	x, y int
}

// find returns the first point of ps whose x is x, and whether there is
// one.
func find(ps []point, x int) (point, bool) {
	for _, p := range ps {
		if p.x == x {
			return p, true
		}
	}
	return point{}, false
}

// origin returns the origin, an empty pair and no error. Every result is
// the zero value of its type.
func origin() (point, [2]int, error) {
	return point{x: 0}, [2]int{}, nil
}

// first returns the first value of xs, or the zero value of T.
func first[T any](xs []T) T {
	if len(xs) > 0 {
		return xs[0]
	}
	return *new(T)
}

// empty returns an empty list, which is not nil.
func empty() []int {
	return []int{}
}
