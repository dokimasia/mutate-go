package fixture

import "log"

// report logs n plus one and returns n times two.
func report(n int) int {
	log.Printf("n+1 = %d", n+1)
	return n * 2
}

// room returns an empty list with room for n+1 values.
func room(n int) []int {
	return make([]int, 0, n+1)
}
