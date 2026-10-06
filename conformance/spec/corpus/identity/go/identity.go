package fixture

// steps counts n down by one twice, and not below zero.
func steps(n int) int {
	if n > 0 {
		n--
	}
	if n > 0 {
		n--
	}
	return n
}

// counter is a count.
type counter struct {
	n int
}

// add returns the count plus one. Its receiver's type is in parentheses.
// gofmt removes such parentheses, so this file is kept as it is written.
func (c (*counter)) add() int {
	return c.n + 1
}

// sub returns the count minus one. Its receiver's type is in parentheses.
func (c (counter)) sub() int {
	return c.n - 1
}

// twice returns the count times two. The type that its receiver points to
// is in parentheses.
func (c *(counter)) twice() int {
	return c.n * 2
}
