package fixture

import (
	"strconv"
	"testing"
)

// lane has a method Parallel of its own, which marks no test.
type lane struct {
	parallel bool
}

// Parallel marks l as parallel.
func (l *lane) Parallel() {
	l.parallel = true
}

// each runs check on each number of numbers, in a parallel subtest of t.
func each(t *testing.T, numbers []int, check func(t *testing.T, n int)) {
	for _, n := range numbers {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()
			check(t, n)
		})
	}
}

// open returns a lane that Parallel marked.
func open() *lane {
	l := &lane{}
	l.Parallel()
	return l
}
