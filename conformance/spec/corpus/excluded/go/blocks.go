package fixture

// size is the size of one block in bytes.
const size = 4 * 1024

// blocks returns the size of n blocks in bytes.
func blocks(n int) int {
	return n * size
}
