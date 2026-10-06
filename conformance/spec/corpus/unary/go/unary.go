package fixture

// negate returns x with the opposite sign.
func negate(x int) int {
	return -x
}

// flip returns the opposite of ok.
func flip(ok bool) bool {
	return !ok
}

// count returns n plus one.
func count(n int) int {
	n++
	return n
}

// toggle sets *b to true through parentheses, and returns whether *b was
// false.
func toggle(b *bool) bool {
	was := *b
	(*b) = true
	return !(was)
}

// address returns the address of ok, in parentheses.
func address(ok bool) *bool {
	return &(ok)
}
