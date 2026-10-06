// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/render"
)

// startLine is the first line of a trace, which the instrumented package
// writes when it initializes.
const startLine = "start\n"

func TestTrace(t *testing.T) {
	t.Parallel()

	t.Run("ParseTrace", func(t *testing.T) {
		t.Parallel()

		t.Run("reads the trace that the instrumented binary writes", func(t *testing.T) {
			t.Parallel()
			p, r := fixture(t, map[string]string{
				"f.go":      "package fixture\n\nfunc Used(x int) int { return x + 1 }\n\nfunc Unused(x int) int { return x - 1 }\n",
				"f_test.go": "package fixture\n\nimport \"testing\"\n\nfunc TestUsed(t *testing.T) {\n\tUsed(1)\n\tUsed(2)\n}\n",
			})
			_, bin := build(t, p, r)
			trace := filepath.Join(t.TempDir(), "trace")
			run(t, bin, p.Dir, active(0), render.TraceVar+"="+trace)
			data, err := os.ReadFile(trace)
			assert.NoError(t, err, "the binary writes the trace")
			assert.Equal(t, string(data), startLine+"1\n2\n", "the trace states the start and each site of Used once")
			executed, started := render.ParseTrace(data)
			expect.True(t, started, "the trace shows that the instrumented package ran")
			expect.Equal(t, executed, map[int]bool{1: true, 2: true}, "and the first ordinals of Used's two sites")
		})

		t.Run("returns each ordinal that a line states", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "ParseTrace returns the ordinals of the lines that a trace writes", func(c *prop.Case) {
				ordinals := c.Draw(prop.List(prop.Integer(1, 1000), prop.MaxSize(10)), "ordinals")
				var trace strings.Builder
				trace.WriteString(startLine)
				want := map[int]bool{}
				for _, o := range ordinals {
					trace.WriteString(strconv.Itoa(o) + "\n")
					want[o] = true
				}
				executed, started := render.ParseTrace([]byte(trace.String()))
				expect.True(c, started, "a trace that starts with the start line started")
				expect.Equal(c, executed, want, "and states each ordinal of its lines")
			})
		})

		t.Run("reports a trace without the start line as not started", func(t *testing.T) {
			t.Parallel()
			executed, started := render.ParseTrace([]byte("3\nnot an ordinal\n"))
			expect.False(t, started, "the binary did not run the instrumented package")
			expect.Equal(t, executed, map[int]bool{3: true}, "and the lines that state no ordinal count for nothing")
		})
	})
}
