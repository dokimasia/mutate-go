// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package testbin_test

import (
	"fmt"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/bench"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/testbin"
)

// The limits of Scan that the tests pin: the length at which it cuts a
// line, and the length of the tail that it keeps.
const (
	maxLine = 4096
	maxTail = 64 << 10
)

// scanAllocs is the most allocations of a Scan of the output of
// passingTests, measured with testing.AllocsPerRun on Go 1.27.3.
const scanAllocs = 50

// passingTests is the output of 20 tests that pass, each of which writes
// one line.
var passingTests = func() string {
	var b strings.Builder
	for i := range 20 {
		fmt.Fprintf(&b, "\x16=== RUN   Test%02d\n    a_test.go:%d: a line\n\x16--- PASS: Test%02d (0.00s)\n", i, i, i)
	}
	return b.String() + "\x16PASS\n"
}()

// unframed generates a line that a test writes: text that the testing
// package's own lines contain, without the marker that frames them.
var unframed = prop.SampledFrom(
	"--- FAIL: TestFake (0.00s)", "=== RUN   TestFake", "=== PAUSE TestFake", "--- PASS: TestFake (0.00s)",
	"    a_test.go:5: a line", "panic: a test's own panic", "",
)

func TestOutput(t *testing.T) {
	t.Parallel()

	t.Run("Scan", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			give string
			want testbin.Output
		}{
			{
				name: "names each test that a framed FAIL line names once and keeps the line",
				give: "\x16=== RUN   TestA\n    a_test.go:5: broken\n\x16--- FAIL: TestA (0.00s)\n\x16--- FAIL: TestA (0.00s)\n" +
					"\x16=== RUN   TestB\n\x16--- FAIL: TestB (0.00s)\n",
				want: testbin.Output{
					Failed: []string{"TestA", "TestB"},
					Tests:  []string{"TestA", "TestB"},
					Leaves: []string{"TestA", "TestB"},
					Tail:   "    a_test.go:5: broken\n--- FAIL: TestA (0.00s)\n--- FAIL: TestA (0.00s)\n--- FAIL: TestB (0.00s)\n",
				},
			},
			{
				name: "leaves out a failed test whose subtest failed",
				give: "\x16=== RUN   TestA\n\x16=== RUN   TestA/sub\n\x16=== RUN   TestA/sub/case\n" +
					"\x16--- FAIL: TestA/sub/case (0.00s)\n\x16--- FAIL: TestA/sub (0.00s)\n\x16=== RUN   TestA/other\n" +
					"\x16--- FAIL: TestA/other (0.00s)\n\x16--- FAIL: TestA (0.00s)\n",
				want: testbin.Output{
					Failed: []string{"TestA/sub/case", "TestA/other"},
					Tests:  []string{"TestA"},
					Leaves: []string{"TestA/sub/case", "TestA/other"},
					Tail: "--- FAIL: TestA/sub/case (0.00s)\n--- FAIL: TestA/sub (0.00s)\n--- FAIL: TestA/other (0.00s)\n" +
						"--- FAIL: TestA (0.00s)\n",
				},
			},
			{
				name: "names the tests that started and did not end as running in the order that they started",
				give: "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16=== RUN   TestB\n\x16=== PAUSE TestB\n" +
					"\x16=== RUN   TestC\n\x16--- SKIP: TestC (0.00s)\n\x16=== RUN   TestD\n\x16=== PAUSE TestD\n" +
					"\x16=== CONT  TestD\n\x16=== NAME  TestD\n\x16=== CONT  TestB\n",
				want: testbin.Output{
					Running: []string{"TestB", "TestD"},
					Tests:   []string{"TestA", "TestB", "TestC", "TestD"},
					Leaves:  []string{"TestA", "TestB", "TestC", "TestD"},
				},
			},
			{
				name: "leaves out a test that waits for a subtest that started and did not end",
				give: "\x16=== RUN   TestB\n\x16=== PAUSE TestB\n\x16=== RUN   TestC\n\x16=== PAUSE TestC\n" +
					"\x16=== CONT  TestB\n\x16=== RUN   TestB/sub\n\x16=== PAUSE TestB/sub\n\x16=== CONT  TestC\n" +
					"\x16=== RUN   TestC/x\n\x16=== PAUSE TestC/x\n\x16=== CONT  TestB/sub\n\x16=== RUN   TestB/sub/case\n",
				want: testbin.Output{
					Running: []string{"TestB/sub/case"},
					Tests:   []string{"TestB", "TestC"},
					Leaves:  []string{"TestC/x", "TestB/sub/case"},
				},
			},
			{
				name: "names a test whose subtests in progress all paused when no other test runs",
				give: "\x16=== RUN   TestCount\n\x16=== RUN   TestCount/a\n\x16=== PAUSE TestCount/a\n" +
					"\x16=== RUN   TestCount/b\n\x16=== PAUSE TestCount/b\n",
				want: testbin.Output{
					Running: []string{"TestCount"},
					Tests:   []string{"TestCount"},
					Leaves:  []string{"TestCount/a", "TestCount/b"},
				},
			},
			{
				name: "leaves out a parallel test that paused and did not continue",
				give: "\x16=== RUN   TestA\n\x16=== PAUSE TestA\n\x16=== RUN   TestB\n\x16=== PAUSE TestB\n" +
					"\x16=== CONT  TestB\n\x16=== RUN   TestC\n\x16=== PAUSE TestC\n",
				want: testbin.Output{
					Running: []string{"TestB"},
					Tests:   []string{"TestA", "TestB", "TestC"},
					Leaves:  []string{"TestA", "TestB", "TestC"},
				},
			},
			{
				name: "reads the alarm of the testing package",
				give: "\x16=== RUN   TestHang\npanic: test timed out after 2s\nrunning tests:\n\tTestHang (2s)\n",
				want: testbin.Output{
					TimedOut: true, Running: []string{"TestHang"}, Tests: []string{"TestHang"},
					Leaves: []string{"TestHang"},
					Tail:   "panic: test timed out after 2s\nrunning tests:\n\tTestHang (2s)\n",
				},
			},
			{
				name: "lists the top-level tests that started in the order in which they started",
				give: "\x16=== RUN   TestA\n\x16=== RUN   TestA/sub\n\x16--- PASS: TestA/sub (0.00s)\n\x16--- PASS: TestA (0.00s)\n" +
					"\x16=== RUN   ExampleB\n\x16--- PASS: ExampleB (0.00s)\n",
				want: testbin.Output{Tests: []string{"TestA", "ExampleB"}, Leaves: []string{"TestA/sub", "ExampleB"}},
			},
			{
				name: "lists the tests that started no other test as leaves in the order in which they started",
				give: "\x16=== RUN   TestA\n\x16=== RUN   TestA/x\n\x16--- PASS: TestA/x (0.00s)\n\x16=== RUN   TestA/y\n" +
					"\x16=== RUN   TestA/y/1\n\x16--- PASS: TestA/y/1 (0.00s)\n\x16--- PASS: TestA/y (0.00s)\n" +
					"\x16--- PASS: TestA (0.00s)\n\x16=== RUN   TestB\n\x16--- PASS: TestB (0.00s)\n",
				want: testbin.Output{
					Tests:  []string{"TestA", "TestB"},
					Leaves: []string{"TestA/x", "TestA/y/1", "TestB"},
				},
			},
			{
				name: "reports the end of the tests that the last framed PASS line states",
				give: "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16PASS\n",
				want: testbin.Output{Tests: []string{"TestA"}, Leaves: []string{"TestA"}, Complete: true},
			},
			{
				name: "reports the end of the tests that the last framed FAIL line states",
				give: "\x16=== RUN   TestA\n\x16--- FAIL: TestA (0.00s)\n\x16FAIL\n",
				want: testbin.Output{
					Failed: []string{"TestA"}, Tests: []string{"TestA"}, Leaves: []string{"TestA"}, Complete: true,
					Tail: "--- FAIL: TestA (0.00s)\n",
				},
			},
			{
				name: "reports no end of the tests for a PASS line without the framing byte",
				give: "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\nPASS\n",
				want: testbin.Output{Tests: []string{"TestA"}, Leaves: []string{"TestA"}, Tail: "PASS\n"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, testbin.Scan(strings.NewReader(tt.give), nil), tt.want,
					"Scan reads the tests' progress from the framed lines", assert.EquateEmpty())
			})
		}

		t.Run("keeps every line that a test writes and names no test without the framing byte", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "lines without the marker are the tail and state no test", func(c *prop.Case) {
				lines := c.Draw(prop.List(unframed, prop.MaxSize(20)), "lines")
				var give strings.Builder
				for _, line := range lines {
					give.WriteString(line + "\n")
				}
				assert.Equal(c, testbin.Scan(strings.NewReader(give.String()), nil),
					testbin.Output{Tail: give.String()}, "Scan keeps the lines and names no test", assert.EquateEmpty())
			})
		})

		t.Run("calls failed once at the first line that states a failed test", func(t *testing.T) {
			t.Parallel()
			calls := 0
			give := "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16--- FAIL: TestB (0.00s)\n\x16--- FAIL: TestC (0.00s)\n"
			got := testbin.Scan(strings.NewReader(give), func() { calls++ })
			expect.Equal(t, calls, 1, "failed runs once")
			expect.Equal(t, got.Failed, []string{"TestB", "TestC"}, "and Scan reads on")
		})

		t.Run("cuts a line at 4096 bytes", func(t *testing.T) {
			t.Parallel()
			got := testbin.Scan(strings.NewReader(strings.Repeat("x", 5000)+"\nz\n"), nil)
			assert.Equal(t, got.Tail, strings.Repeat("x", maxLine)+"\nz\n", "the tail keeps the start of a long line")
		})

		t.Run("keeps the last 64 KiB of the output", func(t *testing.T) {
			t.Parallel()
			give := strings.Repeat("y\n", 70000) + "\x16--- FAIL: TestLast (0.00s)"
			got := testbin.Scan(strings.NewReader(give), nil)
			expect.Equal(t, got.Failed, []string{"TestLast"}, "the last line states the failure")
			assert.Length(t, got.Tail, maxTail, "the tail is 64 KiB")
			expect.HasSuffix(t, got.Tail, "y\n--- FAIL: TestLast (0.00s)\n", "and ends with the output's end")
		})
	})
}

// TestOutputAllocs counts allocations through testing.AllocsPerRun, so it
// does not run in parallel.
func TestOutputAllocs(t *testing.T) {
	t.Run("Scan", func(t *testing.T) {
		t.Run("allocates for the tests of an output and not for its lines", func(t *testing.T) {
			assert.MaxAllocsWithSetup(t,
				func() *strings.Reader { return strings.NewReader(passingTests) },
				func(r *strings.Reader) { _ = testbin.Scan(r, nil) },
				scanAllocs, "Scan of 20 passing tests that each write a line allocates at most the stated count")
		})
	})
}

func BenchmarkOutput(b *testing.B) {
	b.Run("Scan", func(b *testing.B) {
		var got testbin.Output
		c := bench.Start(b).MaxAllocs(scanAllocs)
		defer c.End()
		for c.Loop() {
			var r *strings.Reader
			c.Excluding(func() { r = strings.NewReader(passingTests) })
			got = testbin.Scan(r, nil)
		}
		assert.Length(b, got.Tests, 20, "Scan reads every test")
	})
}
