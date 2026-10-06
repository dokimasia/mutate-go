package fixture

import (
	"sort"
	"strings"
)

// names lists every name that keep received, in order.
var names []string

// keep adds s to names, sorts them, and returns the length of s.
func keep(s string) int {
	n := len(s)
	names = append(names, s)
	sort.Strings(names)
	total := 0
	total += n
	return total
}

// small reports whether m has a value below 10 for key.
func small(m map[string]int, key string) bool {
	if v, ok := m[key]; ok && v < 10 {
		return true
	}
	return false
}

// size returns the number of bytes of s.
func size(s string) int {
	n := len(s)
	return n
}

// upper returns s in upper case.
func upper(s string) string {
	return strings.ToUpper(s)
}

// length returns the length of a string or of a slice of integers, and -1
// for any other value.
func length(v any) int {
	n := -1
	switch x := v.(type) {
	case string:
		n = len(x)
	case []int:
		n = len(x)
	}
	return n
}

// first returns the index of the first negative value of xs, or the length
// of xs when it has none.
func first(xs []int) int {
	i := 0
scan:
	for ; i < len(xs); i++ {
		if xs[i] < 0 {
			break scan
		}
	}
	return i
}
