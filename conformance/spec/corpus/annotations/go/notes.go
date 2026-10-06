package fixture

// clamp returns x, at most 10.
func clamp(x int) int {
	if x > 10 { //dokimi:mutate-skip ror-boundary: x equal to 10 returns 10 either way
		return 10
	}
	return x
}

// half returns half of x.
func half(x int) int {
	//dokimi:mutate-skip aor
	return x / 2
}

// twice returns x times two.
func twice(x int) int {
	//dokimi:mutate-skip lcr: there is no connector here
	return x * 2
}
