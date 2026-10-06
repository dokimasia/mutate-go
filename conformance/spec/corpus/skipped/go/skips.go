package fixture

import "time"

// flag is a boolean type with a name of its own.
type flag bool

// integer is every type whose underlying type is int or int64.
type integer interface{ ~int | ~int64 }

// number is every integer type and every type whose underlying type is
// float64.
type number interface{ integer | ~float64 }

// sum returns a + b of a number type.
func sum[T number](a, b T) T {
	return a + b
}

// positive reports whether n is above zero.
func positive(n int) flag {
	return n > 0
}

// bump adds n to the first element of xs.
func bump(xs []int, n int) {
	xs[0] += n
}

// later reports whether d is at least 2 to the power of n nanoseconds.
func later(d time.Duration, n uint) bool {
	return d >= 1<<n
}
