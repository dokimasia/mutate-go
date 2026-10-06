// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record_test

import (
	"maps"
	"slices"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/record"
)

// The fields of the inputs digest that the pin states.
const (
	engine    = "v0.1.0"
	toolchain = "go1.27.1"
	buildIDs  = "a/b"
)

// The pins of the two digests. Each was computed with printf and sha256sum,
// outside Go, over the stream that the docblock of its function states:
//
//	printf '%s' 21:dokimi-mutate-files/2 4:a.go 2:d1 7:b/c.txt 2:d2 | sha256sum
//	printf '%s' 22:dokimi-mutate-inputs/2 6:v0.1.0 8:go1.27.1 3:a/b 64:<filesPin> | sha256sum
const (
	filesPin  = "47476b10a7666c5c7da7e573f669cd6cbae4fc2ca51d1a76fa2d9cf444092cc0"
	inputsPin = "sha256:5dec5b4b8efde4fdb4c7c03cc05df151cb3bc50199b6a3ec94f8f593fd0e6554"
)

// buildID is the build ID of the engine's executable in the cases of
// Identity.
const buildID = "x/y"

// alphabet contains the characters of the generated fields, the separator of
// the streams' lengths among them, and outside is a character that it lacks.
const (
	alphabet = "a1:"
	outside  = "x"
)

// pinned is the snapshot whose files digest filesPin states.
var pinned = map[string]string{"a.go": "d1", "b/c.txt": "d2"}

// text generates a short field of a digest's stream.
var text = prop.String(prop.Alphabet(alphabet), prop.MaxSize(4))

func TestInputs(t *testing.T) {
	t.Parallel()

	t.Run("Files", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the digest of each path and file digest behind their lengths", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, record.Files(pinned), filesPin, "the digest covers the stream that the docblock states")
		})

		t.Run("returns one digest in every order of the snapshot's map", func(t *testing.T) {
			t.Parallel()
			prop.Deterministic(
				t,
				func(snapshot map[string]string) (string, error) { return record.Files(snapshot), nil },
				"Files sorts the paths before it hashes them",
				prop.Using(prop.Dict(text, text, prop.MinSize(4), prop.MaxSize(8))),
			)
		})

		t.Run("returns another digest for a changed or renamed file", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "each path and each file digest changes the digest", func(c *prop.Case) {
				snapshot := c.Draw(prop.Dict(text, text, prop.MinSize(1), prop.MaxSize(4)), "snapshot")
				path := c.Draw(prop.SampledFrom(slices.Sorted(maps.Keys(snapshot))...), "path")
				changed := maps.Clone(snapshot)
				changed[path] += outside
				assert.NotEqual(c, record.Files(changed), record.Files(snapshot), "a changed file changes the digest")
				renamed := maps.Clone(snapshot)
				delete(renamed, path)
				renamed[path+outside] = snapshot[path]
				assert.NotEqual(c, record.Files(renamed), record.Files(snapshot), "a renamed file changes the digest")
			})
		})
	})

	t.Run("Inputs", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the digest of each field behind its length", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, record.Inputs(engine, toolchain, buildIDs, filesPin), inputsPin,
				"the digest covers the stream that the docblock states")
		})

		t.Run("returns another digest for fields that differ", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "moving a boundary between the fields changes the digest", func(c *prop.Case) {
				joined := c.Draw(prop.String(prop.Alphabet(alphabet), prop.MaxSize(12)), "joined")
				// cuts generates the three boundaries that split joined into the
				// four fields.
				cuts := prop.List(prop.Integer(0, len(joined)), prop.MinSize(3), prop.MaxSize(3)).Map(
					func(l []int) [3]int { return [3]int(slices.Sorted(slices.Values(l))) },
				)
				inputs := func(at [3]int) string {
					return record.Inputs(joined[:at[0]], joined[at[0]:at[1]], joined[at[1]:at[2]], joined[at[2]:])
				}
				first, second := c.Draw(cuts, "first"), c.Draw(cuts, "second")
				c.Assume(first != second)
				assert.NotEqual(c, inputs(first), inputs(second), "the fields of one text differ in their digests")
			})
		})
	})

	t.Run("Identity", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name, give, want string
		}{
			{"returns a released version alone", "v0.1.0", "v0.1.0"},
			{"adds the build ID to a development build", "(devel)", "(devel) " + buildID},
			{
				"adds the build ID to a build from a modified working tree",
				"v0.1.1-0.20261005120000-0123456789ab+dirty",
				"v0.1.1-0.20261005120000-0123456789ab+dirty " + buildID,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, record.Identity(tt.give, buildID), tt.want, "the identity identifies the engine's code")
			})
		}
	})
}
