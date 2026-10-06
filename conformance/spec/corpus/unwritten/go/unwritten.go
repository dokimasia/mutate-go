package fixture

import "strconv"

// point is a point in the plane.
type point struct {
	x, y int
}

// shift moves p one step to the right.
func (p *point) shift() {
	p.x++
}

// index returns the element of s at i, and whether i is in range. Out of
// range, it returns the zero value of T through a variable that no use
// writes.
func index[T any](s []T, i int) (T, bool) {
	if i < len(s) {
		return s[i], true
	}
	var zero T
	return zero, false
}

// none returns no error through a variable of an interface type that no use
// writes.
func none() error {
	var err error
	return err
}

// assigned returns 3, which an assignment writes.
func assigned() int {
	var n int
	n = 3
	return n
}

// field returns a point whose x an assignment through a field writes.
func field() point {
	var p point
	p.x = 2
	return p
}

// element returns a pair whose first element an assignment through an
// index writes.
func element() [2]int {
	var a [2]int
	a[0] = 4
	return a
}

// declared returns the number in s, which a short variable declaration
// writes, and the error of its parse.
func declared(s string) (int, error) {
	var n int
	n, err := strconv.Atoi(s)
	return n, err
}

// incremented returns 1, which an increment writes.
func incremented() int {
	var n int
	n++
	return n
}

// last returns the last element of xs, which a range clause writes.
func last(xs []int) int {
	var x int
	for _, x = range xs {
	}
	return x
}

// addressed returns 5, which set writes through the variable's address.
func addressed() int {
	var n int
	set(&n)
	return n
}

// set writes 5 through p.
func set(p *int) {
	*p = 5
}

// sliced returns a pair whose bytes a copy into a slice of it writes.
func sliced() [2]byte {
	var a [2]byte
	copy(a[:], "ab")
	return a
}

// shifted returns a point that a method with a pointer receiver writes.
func shifted() point {
	var p point
	p.shift()
	return p
}

// enclosed returns 8, which a function literal writes.
func enclosed() int {
	var n int
	func() { n = 8 }()
	return n
}

// later returns the first element of xs from the loop's second pass, or -1
// for fewer than two elements. The write follows the return in the source,
// and the loop runs it before the return.
func later(xs []int) int {
	var x int
	for i, v := range xs {
		if i > 0 {
			return x
		}
		x = v
	}
	return -1
}
