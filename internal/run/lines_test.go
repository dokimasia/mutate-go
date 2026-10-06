// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/run"
)

func TestLines(t *testing.T) {
	t.Parallel()
	t.Run("ParseLines", func(t *testing.T) {
		t.Parallel()
		t.Run("returns the absolute path and the range of an entry", func(t *testing.T) {
			t.Parallel()
			wd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			got, err := run.ParseLines("sub/a.go:3-9")
			if err != nil || got != (run.Lines{Path: filepath.Join(wd, "sub", "a.go"), First: 3, Last: 9}) {
				t.Errorf("ParseLines() = %+v, %v", got, err)
			}
			if got, err := run.ParseLines("/abs/x:y.go:4-4"); err != nil || got.Path != "/abs/x:y.go" {
				t.Errorf("ParseLines() = %+v, %v, want the file up to the last colon", got, err)
			}
		})
		t.Run("returns an error for an entry that is not file:first-last", func(t *testing.T) {
			t.Parallel()
			for _, give := range []string{"a.go", ":1-2", "a.go:1", "a.go:x-2", "a.go:1-y", "a.go:0-2", "a.go:5-4"} {
				if _, err := run.ParseLines(give); err == nil || !strings.HasPrefix(err.Error(), "run: ") {
					t.Errorf("ParseLines(%q) error = %v", give, err)
				}
			}
		})
	})
}
