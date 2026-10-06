// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"reflect"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/run"
)

func TestOutput(t *testing.T) {
	t.Parallel()
	t.Run("Scan", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			give     string
			want     run.Output
			wantTail string
		}{
			{
				"names each test that a framed FAIL line names, once, and keeps the line",
				"\x16=== RUN   TestA\n\x16=== RUN   TestA/sub\n    a_test.go:5: broken\n\x16--- FAIL: TestA/sub (0.00s)\n" +
					"\x16--- FAIL: TestA (0.00s)\n\x16--- FAIL: TestA (0.00s)\n\x16FAIL\n",
				run.Output{Failed: []string{"TestA/sub", "TestA"}, Tests: []string{"TestA"}},
				"    a_test.go:5: broken\n--- FAIL: TestA/sub (0.00s)\n--- FAIL: TestA (0.00s)\n--- FAIL: TestA (0.00s)\n",
			},
			{
				"names the tests that started and did not end as running, in the order that they started",
				"\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16=== RUN   TestB\n\x16=== PAUSE TestB\n" +
					"\x16=== RUN   TestC\n\x16--- SKIP: TestC (0.00s)\n\x16=== RUN   TestD\n\x16=== PAUSE TestD\n" +
					"\x16=== CONT  TestD\n\x16=== NAME  TestD\n\x16=== CONT  TestB\n",
				run.Output{Running: []string{"TestB", "TestD"}, Tests: []string{"TestA", "TestB", "TestC", "TestD"}},
				"",
			},
			{
				"leaves out a test that waits for a subtest that started and did not end",
				"\x16=== RUN   TestB\n\x16=== PAUSE TestB\n\x16=== RUN   TestC\n\x16=== PAUSE TestC\n" +
					"\x16=== CONT  TestB\n\x16=== RUN   TestB/sub\n\x16=== PAUSE TestB/sub\n\x16=== CONT  TestC\n" +
					"\x16=== RUN   TestC/x\n\x16=== PAUSE TestC/x\n\x16=== CONT  TestB/sub\n\x16=== RUN   TestB/sub/case\n",
				run.Output{Running: []string{"TestB/sub/case"}, Tests: []string{"TestB", "TestC"}},
				"",
			},
			{
				"names a test whose subtests in progress all paused when no other test runs",
				"\x16=== RUN   TestCount\n\x16=== RUN   TestCount/a\n\x16=== PAUSE TestCount/a\n" +
					"\x16=== RUN   TestCount/b\n\x16=== PAUSE TestCount/b\n",
				run.Output{Running: []string{"TestCount"}, Tests: []string{"TestCount"}},
				"",
			},
			{
				"leaves out a parallel test that paused and did not continue",
				"\x16=== RUN   TestA\n\x16=== PAUSE TestA\n\x16=== RUN   TestB\n\x16=== PAUSE TestB\n" +
					"\x16=== CONT  TestB\n\x16=== RUN   TestC\n\x16=== PAUSE TestC\n",
				run.Output{Running: []string{"TestB"}, Tests: []string{"TestA", "TestB", "TestC"}},
				"",
			},
			{
				"reads the alarm of the testing package",
				"\x16=== RUN   TestHang\npanic: test timed out after 2s\nrunning tests:\n\tTestHang (2s)\n",
				run.Output{TimedOut: true, Running: []string{"TestHang"}, Tests: []string{"TestHang"}},
				"panic: test timed out after 2s\nrunning tests:\n\tTestHang (2s)\n",
			},
			{
				"lists the top-level tests that started, in the order in which they started",
				"\x16=== RUN   TestA\n\x16=== RUN   TestA/sub\n\x16--- PASS: TestA/sub (0.00s)\n\x16--- PASS: TestA (0.00s)\n" +
					"\x16=== RUN   ExampleB\n\x16--- PASS: ExampleB (0.00s)\n",
				run.Output{Tests: []string{"TestA", "ExampleB"}},
				"",
			},
			{
				"ignores the text of a state line that a test writes without the framing byte",
				"--- FAIL: TestFake (0.00s)\n=== RUN   TestFake\n",
				run.Output{},
				"--- FAIL: TestFake (0.00s)\n=== RUN   TestFake\n",
			},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := run.Scan(strings.NewReader(tt.give), nil)
				if tt.want.Tail = tt.wantTail; !reflect.DeepEqual(got, tt.want) {
					t.Errorf("Scan() = %+v, want %+v", got, tt.want)
				}
			})
		}
		t.Run("calls failed once, at the first line that states a failed test", func(t *testing.T) {
			t.Parallel()
			var calls []int
			give := "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16--- FAIL: TestB (0.00s)\n\x16--- FAIL: TestC (0.00s)\n"
			got := run.Scan(strings.NewReader(give), func() { calls = append(calls, 1) })
			if len(calls) != 1 || !reflect.DeepEqual(got.Failed, []string{"TestB", "TestC"}) {
				t.Errorf("failed ran %d times, and Scan() names %v", len(calls), got.Failed)
			}
		})
		t.Run("cuts a line at 4096 bytes and keeps the last 64 KiB", func(t *testing.T) {
			t.Parallel()
			long := strings.Repeat("x", 5000)
			give := long + "\n" + strings.Repeat("y\n", 70000) + "\x16--- FAIL: TestLast (0.00s)"
			got := run.Scan(strings.NewReader(give), nil)
			if !reflect.DeepEqual(got.Failed, []string{"TestLast"}) || len(got.Tail) != 64<<10 ||
				!strings.HasSuffix(got.Tail, "y\n--- FAIL: TestLast (0.00s)\n") {
				t.Errorf(
					"Scan() names %v and keeps a tail of %d bytes ending %q",
					got.Failed,
					len(got.Tail),
					got.Tail[len(got.Tail)-40:],
				)
			}
			first := run.Scan(strings.NewReader(long+"\nz\n"), nil)
			if first.Tail != strings.Repeat("x", 4096)+"\nz\n" {
				t.Errorf("Scan() keeps %d bytes of a 5000-byte line", strings.Index(first.Tail, "\n"))
			}
		})
	})
}
