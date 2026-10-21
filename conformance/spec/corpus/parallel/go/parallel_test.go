package fixture

import (
	"sync"
	"testing"
)

func TestEach(t *testing.T) {
	var mu sync.Mutex
	sum := 0
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if sum != 3 {
			t.Errorf("the checks added up to %d, want 3", sum)
		}
	})
	each(t, []int{1, 2}, func(t *testing.T, n int) {
		mu.Lock()
		defer mu.Unlock()
		sum += n
	})
}

func TestOpen(t *testing.T) {
	if !open().parallel {
		t.Fatal("open returned a lane that Parallel did not mark")
	}
}
