// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate_test

import "testing"

// quiet is a fixture of compound statements whose bodies log, with and
// without other effects in their bodies and headers.
const quiet = `package fixture

import (
	"fmt"
	"io"
	"log"
	"time"
)

func warn(err error) {
	if err != nil {
		log.Printf("warning: %v", err)
	}
}

func fail(err error) error {
	if err != nil {
		log.Printf("failure: %v", err)
		return fmt.Errorf("fail: %w", err)
	}
	return nil
}

func each(xs []int) {
	for _, x := range xs {
		log.Printf("value: %d", x)
	}
}

func drain(ch chan int) {
	for x := range ch {
		log.Printf("value: %d", x)
	}
}

func flush(w io.Writer, buf []byte) {
	if _, err := w.Write(buf); err != nil {
		log.Printf("flush: %v", err)
	}
}

func probe(w io.Writer, ready func() bool) {
	if _, err := w.Write(nil); err != nil && ready() {
		log.Print("not ready")
	}
}

func level(check func() error) {
	switch err := check(); {
	case err == nil:
		log.Print("ok")
	default:
		log.Printf("failed: %v", err)
	}
}

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

func report(ok bool, d time.Duration) {
	if ok {
		log.Print("ok")
	} else if d > 0 {
		time.Sleep(d)
	}
}

func count(xs []int) (n int) {
	for i := 0; i < len(xs); i++ {
		log.Print(xs[i])
	}
	for n = 0; n < len(xs); n++ {
		log.Print(xs[n])
	}
	return n
}

func nothing(ok bool) {
	if ok {
	}
}
`

func TestQuiet(t *testing.T) {
	t.Parallel()
	t.Run("Enumerate", func(t *testing.T) {
		t.Parallel()
		t.Run("suppresses each compound statement whose bodies only make calls of a family", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"quiet.go": quiet})
			want(t, listing(r, ""), `warn sbr-delete 0: if err != nil { log.Printf("warning: %v", err) } -> "" [logging]
warn ror-true 0: err != nil -> "true" [logging]
warn ror-false 0: err != nil -> "false" [logging]
warn sbr-delete 1: log.Printf("warning: %v", err) -> "" [logging]
fail sbr-delete 0: if err != nil { log.Printf("failure: %v", err) return fmt.Errorf("fail: %w", err) } -> ""
fail ror-true 0: err != nil -> "true"
fail ror-false 0: err != nil -> "false"
fail sbr-delete 1: log.Printf("failure: %v", err) -> "" [logging]
fail sbr-zero 0: return fmt.Errorf("fail: %w", err) -> "return nil"
each sbr-delete 0: for _, x := range xs { log.Printf("value: %d", x) } -> "" [logging]
each sbr-delete 1: log.Printf("value: %d", x) -> "" [logging]
drain sbr-delete 0: for x := range ch { log.Printf("value: %d", x) } -> ""
drain sbr-delete 1: log.Printf("value: %d", x) -> "" [logging]
flush sbr-delete 0: if _, err := w.Write(buf); err != nil { log.Printf("flush: %v", err) } -> ""
flush ror-true 0: err != nil -> "true" [logging]
flush ror-false 0: err != nil -> "false" [logging]
flush sbr-delete 1: log.Printf("flush: %v", err) -> "" [logging]
probe sbr-delete 0: if _, err := w.Write(nil); err != nil && ready() { log.Print("not ready") } -> ""
probe lcr-left 0: err != nil && ready() -> "err != nil"
probe lcr-right 0: err != nil && ready() -> "ready()"
probe lcr-false 0: err != nil && ready() -> "false"
probe ror-true 0: err != nil -> "true"
probe ror-false 0: err != nil -> "false"
probe sbr-delete 1: log.Print("not ready") -> "" [logging]
level sbr-delete 0: switch err := check(); { case err == nil: log.Print("ok") default: log.Printf("failed: %v", err) } -> ""
level ror-true 0: err == nil -> "true" [logging]
level ror-false 0: err == nil -> "false" [logging]
level sbr-delete 1: log.Print("ok") -> "" [logging]
level sbr-delete 2: log.Printf("failed: %v", err) -> "" [logging]
sign sbr-delete 0: switch { case n > 0: log.Print("positive") case n < 0: log.Print("negative") default: log.Print("zero") } -> "" [logging]
sign ror-boundary 0: n > 0 -> "n >= 0" [logging]
sign ror-false 0: n > 0 -> "false" [logging]
sign sbr-delete 1: log.Print("positive") -> "" [logging]
sign ror-boundary 1: n < 0 -> "n <= 0" [logging]
sign ror-false 1: n < 0 -> "false" [logging]
sign sbr-delete 2: log.Print("negative") -> "" [logging]
sign sbr-delete 3: log.Print("zero") -> "" [logging]
report sbr-delete 0: if ok { log.Print("ok") } else if d > 0 { time.Sleep(d) } -> "" [logging]
report uoi-not 0: ok -> "!ok" [logging]
report sbr-delete 1: log.Print("ok") -> "" [logging]
report ror-boundary 0: d > 0 -> "d >= 0" [logging]
report ror-false 0: d > 0 -> "false" [logging]
report sbr-delete 2: time.Sleep(d) -> "" [timing]
count sbr-delete 0: for i := 0; i < len(xs); i++ { log.Print(xs[i]) } -> "" [logging]
count ror-boundary 0: i < len(xs) -> "i <= len(xs)" [logging]
count ror-false 0: i < len(xs) -> "false" [logging]
count uoi-incdec 0: i++ -> "i--" [logging]
count sbr-delete 1: log.Print(xs[i]) -> "" [logging]
count sbr-delete 2: for n = 0; n < len(xs); n++ { log.Print(xs[n]) } -> ""
count ror-boundary 1: n < len(xs) -> "n <= len(xs)"
count ror-false 1: n < len(xs) -> "false"
count uoi-incdec 1: n++ -> "n--"
count sbr-delete 3: log.Print(xs[n]) -> "" [logging]
count sbr-zero 0: return n -> "return 0"
nothing sbr-delete 0: if ok { } -> ""
nothing uoi-not 0: ok -> "!ok"
`)
		})
		t.Run("suppresses only a compound statement whose every part has no effect", func(t *testing.T) {
			t.Parallel()
			r := enumerateFixture(t, map[string]string{"edges.go": edges})
			want(t, listing(r, "sbr-delete"), edgeDeletions)
		})
	})
}

// edges is a fixture with each kind of compound statement, header and body
// that the rule of quiet statements tells apart.
const edges = `package fixture

import "log"

func labelled(xs []int) {
	if len(xs) == 0 {
		goto L
	}
L:
	for i := 0; i < len(xs); i++ {
		log.Print(xs[i])
	}
}

func block(n int) {
	{
		log.Print(n)
	}
}

func empty(ok bool) {
	if ok {
		;
		log.Print("ok")
	}
}

func loops(n, j int) {
	for n > 0 {
		log.Print(n)
	}
	for i := 0; i < n; i += 2 {
		log.Print(i)
	}
	for i := 0; i < n; j += 1 {
		log.Print(i)
	}
	for i := 0; i < n; log.Print(i) {
	}
}

func ranges(xs []int, y int) {
	for _, y = range xs {
		log.Print(y)
	}
}

func cases(n int, f func() bool) {
	switch n {
	case 1:
		log.Print("one")
	}
	switch {
	case f():
		log.Print("f")
	}
	switch f() {
	case true:
		log.Print("true")
	}
	switch {
	case n > 1:
		return
	}
}

func types(x any, g func() any) {
	switch v := x.(type) {
	case int:
		log.Print(v)
	}
	switch v := g().(type) {
	case int:
		log.Print(v)
	}
	switch x.(type) {
	case string:
		return
	}
}

func headers(ch chan bool, a, b []int, n int, m map[int]int) {
	if <-ch {
		log.Print("received")
	}
	if copy(a, b) > 0 {
		log.Print("copied")
	}
	if int64(n) > 0 {
		log.Print("positive")
	}
	if f := func() bool { return n > 0 }; f != nil {
		log.Print("func")
	}
	if v, ok := m[n]; ok {
		log.Print(v)
	}
}

func others(ch chan int, f func()) {
	if len(ch) > 0 {
		<-ch
	}
	if f != nil {
		f()
	}
	if f != nil {
		defer log.Print("deferred")
	}
	if f != nil {
		go log.Print("started")
	}
	select {
	case v := <-ch:
		log.Print(v)
	}
}
`

// edgeDeletions lists the deletions of edges. A deletion with the rule
// logging is part of a suppressed statement, or the deletion of a call.
const edgeDeletions = `labelled sbr-delete 0: if len(xs) == 0 { goto L } -> ""
labelled sbr-delete 1: log.Print(xs[i]) -> "" [logging]
block sbr-delete 0: { log.Print(n) } -> "" [logging]
block sbr-delete 1: log.Print(n) -> "" [logging]
empty sbr-delete 0: if ok { ; log.Print("ok") } -> "" [logging]
empty sbr-delete 1: log.Print("ok") -> "" [logging]
loops sbr-delete 0: for n > 0 { log.Print(n) } -> "" [logging]
loops sbr-delete 1: log.Print(n) -> "" [logging]
loops sbr-delete 2: for i := 0; i < n; i += 2 { log.Print(i) } -> "" [logging]
loops sbr-delete 3: log.Print(i) -> "" [logging]
loops sbr-delete 4: for i := 0; i < n; j += 1 { log.Print(i) } -> ""
loops sbr-delete 5: log.Print(i) -> "" [logging]
loops sbr-delete 6: for i := 0; i < n; log.Print(i) { } -> ""
ranges sbr-delete 0: for _, y = range xs { log.Print(y) } -> ""
ranges sbr-delete 1: log.Print(y) -> "" [logging]
cases sbr-delete 0: switch n { case 1: log.Print("one") } -> "" [logging]
cases sbr-delete 1: log.Print("one") -> "" [logging]
cases sbr-delete 2: switch { case f(): log.Print("f") } -> ""
cases sbr-delete 3: log.Print("f") -> "" [logging]
cases sbr-delete 4: switch f() { case true: log.Print("true") } -> ""
cases sbr-delete 5: log.Print("true") -> "" [logging]
types sbr-delete 0: switch v := x.(type) { case int: log.Print(v) } -> "" [logging]
types sbr-delete 1: log.Print(v) -> "" [logging]
types sbr-delete 2: switch v := g().(type) { case int: log.Print(v) } -> ""
types sbr-delete 3: log.Print(v) -> "" [logging]
headers sbr-delete 0: if <-ch { log.Print("received") } -> ""
headers sbr-delete 1: log.Print("received") -> "" [logging]
headers sbr-delete 2: if copy(a, b) > 0 { log.Print("copied") } -> ""
headers sbr-delete 3: log.Print("copied") -> "" [logging]
headers sbr-delete 4: if int64(n) > 0 { log.Print("positive") } -> "" [logging]
headers sbr-delete 5: log.Print("positive") -> "" [logging]
headers sbr-delete 6: if f := func() bool { return n > 0 }; f != nil { log.Print("func") } -> "" [logging]
headers sbr-delete 7: log.Print("func") -> "" [logging]
headers sbr-delete 8: if v, ok := m[n]; ok { log.Print(v) } -> "" [logging]
headers sbr-delete 9: log.Print(v) -> "" [logging]
others sbr-delete 0: if len(ch) > 0 { <-ch } -> ""
others sbr-delete 1: <-ch -> ""
others sbr-delete 2: if f != nil { f() } -> ""
others sbr-delete 3: f() -> ""
others sbr-delete 4: if f != nil { defer log.Print("deferred") } -> "" [logging]
others sbr-delete 5: defer log.Print("deferred") -> "" [logging]
others sbr-delete 6: if f != nil { go log.Print("started") } -> "" [logging]
others sbr-delete 7: go log.Print("started") -> "" [logging]
others sbr-delete 8: select { case v := <-ch: log.Print(v) } -> ""
others sbr-delete 9: log.Print(v) -> "" [logging]
`
