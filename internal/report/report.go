// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/spec"
)

// The words before the count of the mutants that a summary counts: all of
// the run's, or those of its sample.
const (
	ofAll    = "of "
	ofSample = "of a sample of "
)

// The verbs that join a mutant's original code to its replacement: in the
// line of a run, and in the line of a listing.
const (
	became  = "became"
	becomes = "becomes"
)

// toTest is the status of a mutant in a listing that a run would test,
// which the listing leaves without a verdict.
const toTest = "to test"

// closingIncomplete is the note on a run whose closing control run did not
// complete, which fails the run.
const closingIncomplete = "the closing control run did not complete, so no run checked that the mutant runs left " +
	"the tests' state intact"

// Listed reports whether a report lists a mutant with the verdict v on a
// line of its own: a mutant that protocol counts as undetected, a survivor
// or a mutant without coverage, or a mutant whose run ended in an error.
func Listed(protocol spec.Protocol, v spec.Verdict) bool {
	return protocol.Class(v) == spec.Undetected || v == spec.Error
}

// Line returns the line that reports m at its position in the file at path:
//
//	path:line:column: verdict: original became replacement (kind)
//
// A mutant with an empty replacement reads "original removed", and the line
// of a mutant whose run ended in an error ends with the reason.
func Line(m record.Mutant, path string) string {
	text := line(m, path, phrase(m.Verdict), became)
	if m.Verdict == spec.Error && m.Reason != "" {
		text += ": " + m.Reason
	}
	return text
}

// Planned returns the line that lists m, a mutant of a listing, at its
// position in the file at path:
//
//	path:line:column: status: original becomes replacement (kind)
//
// The status of a mutant without a verdict, which a run would test, is "to
// test". The status of any other mutant is its verdict, followed by the
// rule family of a suppressed mutant in parentheses. A mutant with an empty
// replacement reads "original removed".
func Planned(m record.Mutant, path string) string {
	status := phrase(m.Verdict)
	switch {
	case m.Verdict == "":
		status = toTest
	case m.Rule != "":
		status += " (" + string(m.Rule) + ")"
	}
	return line(m, path, status, becomes)
}

// Notes returns the notes on rec besides the mutants' lines and the
// summary: one line for each run error, as its code and its message, one
// line for each reason for which mutants did not run, with their number, in
// the order of each reason's first mutant, and one line for a closing
// control run that did not complete, unless a run error states its
// failure. It returns nil for a run without any of them.
func Notes(rec *record.Record) []string {
	var notes []string
	closingFailed := false
	for _, e := range rec.Errors {
		notes = append(notes, string(e.Code)+": "+e.Message)
		closingFailed = closingFailed || e.Code == spec.ErrorClosing
	}
	var reasons []string
	notRun := map[string]int{}
	for _, m := range rec.Mutants {
		if m.Verdict != spec.NotRun {
			continue
		}
		if notRun[m.Reason] == 0 {
			reasons = append(reasons, m.Reason)
		}
		notRun[m.Reason]++
	}
	for _, reason := range reasons {
		notes = append(notes, count(notRun[reason], "mutant")+" did not run: "+reason)
	}
	if rec.Control != nil && rec.Control.Closing == nil && !closingFailed {
		notes = append(notes, closingIncomplete)
	}
	return notes
}

// Summary returns one line that states the run:
//
//   - the target
//   - how many of the mutants that the score counts the tests detected, as
//     a count and as a percentage rounded down; for a run that the caller's
//     limit ended, those of its sample; and for a run that fails, that it
//     failed, with the detected mutants of a sample that is not empty
//   - the count of each verdict, in protocol's order
//   - the number of the generated files without the include directive and
//     of their mutants, and whether the run included them, when the package
//     has such a file
func Summary(rec *record.Record, protocol spec.Protocol) string {
	detected, undetected := rec.Tally(protocol, "")
	sampled := rec.Sample != nil && rec.Sample.Score != nil
	head := "no mutant to detect"
	switch {
	case rec.Failed() && sampled:
		head = "the run failed, " + detection(rec.Sample.Detected, rec.Sample.Undetected, ofSample)
	case rec.Failed():
		head = "the run failed"
	case sampled:
		head = detection(rec.Sample.Detected, rec.Sample.Undetected, ofSample)
	case rec.Sample == nil && detected+undetected > 0:
		head = detection(detected, undetected, ofAll)
	}
	text := rec.Target.Name + ": " + head
	if counts := parts(rec, protocol); len(counts) > 0 {
		text += ": " + strings.Join(counts, ", ")
	}
	return text
}

// Plan returns one line that counts the mutants of a listing: the target,
// the mutants to test, the count of each verdict in protocol's order, and
// the generated files as Summary states them.
//
//	example.com/arith: 6 mutants to test, 2 suppressed, 1 not viable
func Plan(rec *record.Record, protocol spec.Protocol) string {
	tests := 0
	for _, m := range rec.Mutants {
		if m.Verdict == "" {
			tests++
		}
	}
	head := count(tests, "mutant") + " " + toTest
	return rec.Target.Name + ": " + strings.Join(append([]string{head}, parts(rec, protocol)...), ", ")
}

// line returns the line of m at its position in the file at path, with the
// status, and the change of the original code, which verb joins to the
// replacement.
func line(m record.Mutant, path, status, verb string) string {
	change := m.Original + " " + verb + " " + m.Replacement
	if m.Replacement == "" {
		change = m.Original + " removed"
	}
	return fmt.Sprintf("%s:%d:%d: %s: %s (%s)", path, m.Start.Line, m.Start.Column, status, change, m.Kind)
}

// parts returns the parts of a line that counts rec's mutants: the count of
// each verdict, in protocol's order, and the generated files without the
// include directive, with their mutants and whether the run included them.
func parts(rec *record.Record, protocol spec.Protocol) []string {
	counts := map[spec.Verdict]int{}
	for _, m := range rec.Mutants {
		counts[m.Verdict]++
	}
	var list []string
	for _, v := range protocol.Verdicts {
		if counts[v.ID] > 0 {
			list = append(list, strconv.Itoa(counts[v.ID])+" "+phrase(v.ID))
		}
	}
	if len(rec.Generated) > 0 {
		mutants := 0
		for _, g := range rec.Generated {
			mutants += g.Mutants
		}
		fate := "left out"
		if rec.Generated[0].Included {
			fate = "included"
		}
		list = append(list, count(len(rec.Generated), "generated file")+" with "+count(mutants, "mutant")+" "+fate)
	}
	return list
}

// phrase returns a verdict as text writes it: not covered for no-coverage,
// and the verdict with spaces in place of its hyphens for any other.
func phrase(verdict spec.Verdict) string {
	if verdict == spec.NoCoverage {
		return "not covered"
	}
	return strings.ReplaceAll(string(verdict), "-", " ")
}

// detection states the detected mutants among the detected and the
// undetected ones, and the percentage rounded down, as in "12 of 13 mutants
// detected (92%)". of is the words before the count of the mutants, which
// is at least 1.
func detection(detected, undetected int, of string) string {
	counted := detected + undetected
	return fmt.Sprintf("%d %s%s detected (%d%%)", detected, of, count(counted, "mutant"), detected*100/counted)
}

// count returns n and noun, in the plural unless n is 1.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
