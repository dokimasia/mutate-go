// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package testbin_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"
	"go.dokimi.dev/assert/prop"

	"go.dokimi.dev/mutate/internal/testbin"
)

// runFlag starts the flag that Only returns.
const runFlag = "-test.run="

// testName generates the name of a top-level test, with the characters
// that a regular expression reads as operators.
var testName = prop.String(prop.Alphabet("Test_x.*+?()[]{}|^$\\"), prop.MinSize(1), prop.MaxSize(8))

// variable generates an entry of an environment: a key of a few letters, or
// a key and a value.
var variable = prop.Composite(func(c *prop.Case) string {
	key := c.Draw(prop.SampledFrom("A", "B", "C", "PATH"), "key")
	if c.Draw(prop.Boolean(), "set") {
		return key + "=" + c.Draw(prop.String(prop.Alphabet("xy="), prop.MaxSize(3)), "value")
	}
	return key
})

// keyOf returns the key of an entry of an environment.
func keyOf(entry string) string {
	key, _, _ := strings.Cut(entry, "=")
	return key
}

func TestArgs(t *testing.T) {
	t.Parallel()

	t.Run("Flags", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			failfast bool
			want     []string
		}{
			{
				name: "returns the framed progress, a failure on exit 0 and the deadline",
				want: []string{"-test.v=test2json", "-test.paniconexit0", "-test.timeout=1m30s"},
			},
			{
				name:     "adds failfast",
				failfast: true,
				want:     []string{"-test.v=test2json", "-test.paniconexit0", "-test.timeout=1m30s", "-test.failfast"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, testbin.Flags(90*time.Second, tt.failfast), tt.want,
					"the flags state the run's framing, its exit rule and its deadline")
			})
		}
	})

	t.Run("Only", func(t *testing.T) {
		t.Parallel()

		t.Run("returns a pattern that matches each name in full", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "the pattern of Only matches each of its names in full", func(c *prop.Case) {
				names := c.Draw(prop.List(testName, prop.MinSize(1), prop.MaxSize(4)), "names")
				flag := testbin.Only(names...)
				assert.HasPrefix(c, flag, runFlag, "Only returns the flag that selects tests")
				pattern, err := regexp.Compile(strings.TrimPrefix(flag, runFlag))
				assert.NoError(c, err, "the pattern compiles")
				for _, name := range names {
					expect.True(c, pattern.MatchString(name), "the pattern matches "+name)
					// A longer name that names lists is one of the names.
					if longer := name + "x"; !slices.Contains(names, longer) {
						expect.False(c, pattern.MatchString(longer), "and no name that the list lacks")
					}
				}
			})
		})

		t.Run("returns a pattern that reads each name literally", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testbin.Only("TestA", "Test.B"), `-test.run=^(TestA|Test\.B)$`,
				"the dot of a name is no operator")
		})
	})

	t.Run("Setenv", func(t *testing.T) {
		t.Parallel()

		t.Run(
			"returns the environment with the entries of kv in place of every entry of their keys",
			func(t *testing.T) {
				t.Parallel()
				prop.ForAll(t, "the entries of kv replace their keys and follow the other entries", func(c *prop.Case) {
					env := c.Draw(prop.List(variable, prop.MaxSize(6)), "env")
					kv := c.Draw(prop.List(variable, prop.MaxSize(3)), "kv")
					got := testbin.Setenv(env, kv...)
					keys := map[string]bool{}
					for _, e := range kv {
						keys[keyOf(e)] = true
					}
					var kept, set []string
					for _, e := range got {
						if keys[keyOf(e)] {
							set = append(set, e)
						} else {
							kept = append(kept, e)
						}
					}
					expect.Equal(
						c,
						kept,
						slices.DeleteFunc(slices.Clone(env), func(e string) bool { return keys[keyOf(e)] }),
						"the entries of other keys keep their order",
						assert.EquateEmpty(),
					)
					expect.Equal(
						c,
						set,
						slices.DeleteFunc(slices.Clone(kv), func(e string) bool { return !strings.Contains(e, "=") }),
						"each entry of kv with a value follows them, and a key without one is removed",
						assert.EquateEmpty(),
					)
					expect.Equal(c, got[:len(kept)], kept, "the kept entries come first", assert.EquateEmpty())
				})
			},
		)

		t.Run("leaves its environment unchanged", func(t *testing.T) {
			t.Parallel()
			prop.ForAll(t, "Setenv changes no entry of the environment it receives", func(c *prop.Case) {
				env := c.Draw(prop.List(variable, prop.MaxSize(6)), "env")
				kv := c.Draw(prop.List(variable, prop.MaxSize(3)), "kv")
				assert.Pure(c, func() []string { return slices.Clone(env) }, func() { _ = testbin.Setenv(env, kv...) },
					"the environment reads the same after the call")
			})
		})
	})
}
