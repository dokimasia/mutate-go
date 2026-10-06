// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec_test

import (
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/spec"
)

func TestCatalogue(t *testing.T) {
	t.Parallel()

	t.Run("Kind", func(t *testing.T) {
		t.Parallel()

		t.Run("has a constant for every kind of the catalogue and for no other", func(t *testing.T) {
			t.Parallel()
			var kinds []string
			for _, k := range spec.Load().Catalogue.Kinds {
				kinds = append(kinds, string(k.ID))
			}
			assert.Permutation(t, declared(t, "Kind"), kinds, "the constants of Kind are the catalogue's kinds")
		})
	})

	t.Run("Class", func(t *testing.T) {
		t.Parallel()

		t.Run("has no constant", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, declared(t, "Class"), "the engine reads every class from the catalogue")
		})
	})

	t.Run("Family", func(t *testing.T) {
		t.Parallel()

		t.Run("has no constant", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, declared(t, "Family"), "the engine reads every family from the catalogue")
		})
	})
}
