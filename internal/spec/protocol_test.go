// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec_test

import (
	"slices"
	"testing"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/spec"
)

func TestProtocol(t *testing.T) {
	t.Parallel()

	t.Run("Verdict", func(t *testing.T) {
		t.Parallel()

		t.Run("has a constant for every verdict of the protocol and for no other", func(t *testing.T) {
			t.Parallel()
			var verdicts []string
			for _, v := range spec.Load().Protocol.Verdicts {
				verdicts = append(verdicts, string(v.ID))
			}
			assert.Permutation(
				t,
				declared(t, "Verdict"),
				verdicts,
				"the constants of Verdict are the protocol's verdicts",
			)
		})
	})

	t.Run("ScoreClass", func(t *testing.T) {
		t.Parallel()

		t.Run("has a constant for every class of the score and for no other", func(t *testing.T) {
			t.Parallel()
			var classes []string
			for _, v := range spec.Load().Protocol.Verdicts {
				if !slices.Contains(classes, string(v.Score)) {
					classes = append(classes, string(v.Score))
				}
			}
			assert.Permutation(t, declared(t, "ScoreClass"), classes,
				"the constants of ScoreClass are the classes that the protocol gives its verdicts")
		})
	})

	t.Run("Class", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the class that the protocol gives each verdict", func(t *testing.T) {
			t.Parallel()
			p := spec.Load().Protocol
			for _, v := range p.Verdicts {
				assert.Equal(t, p.Class(v.ID), v.Score, string(v.ID)+" has the protocol's class")
			}
			assert.Equal(t,
				[]spec.ScoreClass{p.Class(spec.Killed), p.Class(spec.NoCoverage), p.Class(spec.NotRun)},
				[]spec.ScoreClass{spec.Detected, spec.Undetected, spec.Excluded},
				"a killed mutant is detected, one without coverage undetected, and one that did not run excluded")
		})

		t.Run("returns the empty class for a verdict that the protocol does not define", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, spec.Load().Protocol.Class(""), "a mutant without a verdict has no class")
		})
	})

	t.Run("ErrorCode", func(t *testing.T) {
		t.Parallel()

		t.Run("has a constant for every run error of the protocol and for no other", func(t *testing.T) {
			t.Parallel()
			var codes []string
			for _, e := range spec.Load().Protocol.Errors {
				codes = append(codes, string(e))
			}
			assert.Permutation(
				t,
				declared(t, "ErrorCode"),
				codes,
				"the constants of ErrorCode are the protocol's run errors",
			)
		})
	})
}
