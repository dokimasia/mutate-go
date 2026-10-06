package fixture

import (
	"io"
	"log"
)

// warn logs err when it is not nil.
func warn(err error) {
	if err != nil {
		log.Printf("warning: %v", err)
	}
}

// fail logs err when it is not nil, and reports whether it is.
func fail(err error) bool {
	if err != nil {
		log.Printf("failure: %v", err)
		return true
	}
	return false
}

// each logs every value of xs.
func each(xs []int) {
	for _, x := range xs {
		log.Printf("value: %d", x)
	}
}

// drain logs every value that ch delivers until ch is closed.
func drain(ch chan int) {
	for x := range ch {
		log.Printf("value: %d", x)
	}
}

// flush writes buf to w, and logs the error of the write.
func flush(w io.Writer, buf []byte) {
	if _, err := w.Write(buf); err != nil {
		log.Printf("flush: %v", err)
	}
}

// level calls check once, and logs whether it returns an error.
func level(check func() error) {
	switch err := check(); {
	case err == nil:
		log.Print("ok")
	default:
		log.Printf("failed: %v", err)
	}
}

// sign logs whether n is positive, negative or zero.
func sign(n int) {
	switch {
	case n > 0:
		log.Print("positive")
	case n < 0:
		log.Print("negative")
	default:
		log.Print("zero")
	}
}
