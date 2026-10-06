package fixture

// weight is the weight of x in each function's sum.
const weight = 0

// outside returns x times weight, plus one. The run's selection leaves
// this function out.
func outside(x int) int {
	y := x * weight //dokimi:mutate-skip aor: outside the selection
	return y + 1
}

// inside returns x times weight, plus one.
func inside(x int) int {
	y := x * weight //dokimi:mutate-skip aor: the compiler rejects the division by weight
	return y + 1
}

// plain returns x times weight, plus one.
func plain(x int) int {
	y := x * weight
	return y + 1
}
