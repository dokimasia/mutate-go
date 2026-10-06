// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec_test

import (
	"maps"
	"slices"
	"testing"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/expect"

	"go.dokimi.dev/mutate/internal/spec"
)

func TestOverlay(t *testing.T) {
	t.Parallel()

	t.Run("SkipReason", func(t *testing.T) {
		t.Parallel()

		t.Run("has a constant for every skip of the overlay and for no other", func(t *testing.T) {
			t.Parallel()
			var reasons []string
			for _, s := range spec.Load().Overlay.Skips {
				reasons = append(reasons, string(s.Reason))
			}
			assert.Permutation(t, declared(t, "SkipReason"), reasons,
				"the constants of SkipReason are the overlay's skip reasons")
		})
	})

	t.Run("Overlay", func(t *testing.T) {
		t.Parallel()

		t.Run("keys the rules of each family of the catalogue by its name", func(t *testing.T) {
			t.Parallel()
			d := spec.Load()
			var families []spec.Family
			for _, f := range d.Catalogue.Families {
				families = append(families, f.ID)
			}
			assert.Permutation(t, slices.Collect(maps.Keys(d.Overlay.Families)), families,
				"the overlay states the rules of the catalogue's families")
		})

		t.Run("has the rules of every kind of the catalogue", func(t *testing.T) {
			t.Parallel()
			d := spec.Load()
			var kinds []spec.Kind
			for _, k := range d.Catalogue.Kinds {
				kinds = append(kinds, k.ID)
			}
			assert.Permutation(t, slices.Collect(maps.Keys(d.Overlay.Kinds)), kinds,
				"the overlay states where each kind of the catalogue applies")
		})
	})

	t.Run("Method", func(t *testing.T) {
		t.Parallel()

		t.Run("matches the methods of an interface in every rule", func(t *testing.T) {
			t.Parallel()
			var methods []spec.Method
			for _, rules := range spec.Load().Overlay.Families {
				methods = append(methods, rules.Methods...)
			}
			assert.NotEmpty(t, methods, "the overlay states a method rule")
			for _, m := range methods {
				expect.Equal(
					t,
					m.On,
					spec.OnInterface,
					"the method rule of "+m.Name+" matches the method of an interface",
				)
			}
		})
	})

	t.Run("Argument", func(t *testing.T) {
		t.Parallel()

		t.Run("names the allocated kind of every rule of make", func(t *testing.T) {
			t.Parallel()
			var kinds []string
			for _, rules := range spec.Load().Overlay.Families {
				for _, a := range rules.Arguments {
					if a.Func == spec.Make {
						kinds = append(kinds, a.Of)
					}
				}
			}
			assert.Permutation(t, kinds, []string{spec.OfSlice, spec.OfMap},
				"the rules of make name a slice and a map, one rule each")
		})
	})
}
