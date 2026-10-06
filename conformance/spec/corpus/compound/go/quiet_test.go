package fixture

import (
	"bytes"
	"errors"
	"testing"
)

func TestWarn(t *testing.T) {
	warn(nil)
	warn(errors.New("lost"))
}

func TestFail(t *testing.T) {
	if fail(nil) || !fail(errors.New("lost")) {
		t.Fatal("fail does not report exactly an error")
	}
}

func TestEach(t *testing.T) {
	each([]int{1, 2})
}

// TestDrain checks that drain receives every value of the channel.
func TestDrain(t *testing.T) {
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	close(ch)
	drain(ch)
	if len(ch) != 0 {
		t.Fatalf("drain left %d values in the channel", len(ch))
	}
}

// TestFlush checks the bytes that flush writes, and not its log.
func TestFlush(t *testing.T) {
	var b bytes.Buffer
	flush(&b, []byte("data"))
	if b.String() != "data" {
		t.Fatalf("flush wrote %q, want data", b.String())
	}
}

// TestLevel checks that level calls check, and not its log.
func TestLevel(t *testing.T) {
	calls := 0
	level(func() error { calls++; return nil })
	level(func() error { calls++; return errors.New("lost") })
	if calls != 2 {
		t.Fatalf("level called check %d times, want 2", calls)
	}
}

func TestSign(t *testing.T) {
	sign(1)
	sign(-1)
	sign(0)
}
