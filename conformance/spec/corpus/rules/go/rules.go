package fixture

import (
	"bytes"
	"log"
)

// report logs n plus one and returns n times two.
func report(n int) int {
	log.Printf("n+1 = %d", n+1)
	return n * 2
}

// room returns an empty list with room for n+1 values.
func room(n int) []int {
	return make([]int, 0, n+1)
}

// buffer returns an empty buffer with room for n+1 bytes, which it grows
// through a method expression.
func buffer(n int) *bytes.Buffer {
	var b bytes.Buffer
	(*bytes.Buffer).Grow(&b, n+1)
	return &b
}
