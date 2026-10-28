// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package record_test

import (
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/record"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrForeign", func(t *testing.T) {
		t.Parallel()

		t.Run("starts with the package's name", func(t *testing.T) {
			t.Parallel()
			assert.HasPrefix(t, record.ErrForeign.Error(), errorPrefix, "the error names its package")
		})
	})
}
