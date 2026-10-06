package fixture

// area returns the area of a rectangle of w by h.
func area(w, h int) int {
	return w * h
}

// perimeter returns the perimeter of a rectangle of w by h.
func perimeter(w, h int) int {
	return 2 * (w + h)
}
