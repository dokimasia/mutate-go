// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"go.dokimi.dev/mutate/internal/spec"
)

// The verdicts, as the protocol spells them.
const (
	Killed      = "killed"
	TimedOut    = "timed-out"
	Exhausted   = "exhausted"
	Survived    = "survived"
	NoCoverage  = "no-coverage"
	NotViable   = "not-viable"
	Suppressed  = "suppressed"
	NotSelected = "not-selected"
	NotRun      = "not-run"
	Error       = "error"
)

// Module is the module path of the engine.
const Module = "go.dokimi.dev/mutate"

// Record is one run of one target. Catalogue and Overlay are the versions
// of the catalogue and of the Go overlay that the run applied, and two
// records' scores are comparable only where both are equal.
type Record struct {
	Record     string `json:"record"`
	Version    int    `json:"version"`
	Catalogue  string `json:"catalogue"`
	Overlay    string `json:"overlay"`
	Engine     Engine `json:"engine"`
	Toolchain  string `json:"toolchain"`
	Target     Target `json:"target"`
	Root       string `json:"root"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	Inputs     string `json:"inputs"`
	// Suite lists the import paths of the other packages whose tests the
	// run added to the package's own. It is nil when the package's own
	// tests are the suite.
	Suite []string `json:"suite"`
	// Selection lists the selected ranges in the package's files. It is nil
	// for a run that selects every line, and empty for a run that selects
	// no line of the package.
	Selection []Range `json:"selection"`
	// Limits lists the limits of each test binary of the suite, in the order
	// of a mutant's run. It is empty when no opening control run passed.
	Limits  []Limits `json:"limits"`
	Control *Control `json:"control,omitempty"`
	// Errors and the other lists are empty, never nil, in a record that
	// Write writes.
	Errors []RunError `json:"errors"`
	Score  *float64   `json:"score"`
	// Sample is nil unless the caller ended the run before every mutant
	// ran.
	Sample  *Sample  `json:"sample"`
	Mutants []Mutant `json:"mutants"`
	Skipped []Skip   `json:"skipped"`
	// Generated lists each generated file of the package that the run
	// leaves out, with the mutants that the kinds make in it.
	Generated []Generated `json:"generated"`
}

// Engine names the engine and its version.
type Engine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Target names the target's language and the target as the language
// spells it.
type Target struct {
	Language string `json:"language"`
	Name     string `json:"name"`
}

// Range is the lines First to Last of File, relative to the root.
type Range struct {
	File  string `json:"file"`
	First int    `json:"first"`
	Last  int    `json:"last"`
}

// Limits are the deadline and the memory ceiling of each run of one test
// binary, which runs the tests of the package Target. MemoryCeilingBytes is
// nil where the engine does not apply a ceiling.
type Limits struct {
	Target             string  `json:"target"`
	DeadlineSeconds    float64 `json:"deadlineSeconds"`
	MemoryCeilingBytes *int64  `json:"memoryCeilingBytes"`
}

// Control states the control runs. Ordinary is nil unless the run
// confirms its survivors, and Closing is nil when the run ended before its
// closing control run.
type Control struct {
	Opening  Opening   `json:"opening"`
	Ordinary *Ordinary `json:"ordinary,omitempty"`
	Closing  *Closing  `json:"closing,omitempty"`
}

// Opening states the opening control run: its wall time, its peak resident
// memory where the platform measures it, the instrumented sites, and the
// sites that executed.
type Opening struct {
	Seconds       float64 `json:"seconds"`
	PeakBytes     *int64  `json:"peakBytes"`
	Sites         int     `json:"sites"`
	SitesExecuted int     `json:"sitesExecuted"`
}

// Ordinary states the wall time of the ordinary control run: the suite
// built from the package's unchanged code, without the engine.
type Ordinary struct {
	Seconds float64 `json:"seconds"`
}

// Closing states the closing control run's wall time.
type Closing struct {
	Seconds float64 `json:"seconds"`
}

// Sample states the mutants that the score counts and whose keys sort
// before Before, the first key of a mutant that did not run. A run starts
// the mutants in the order of their keys, so the sample is a uniform
// sample of the target's mutants. Score is nil for an empty sample.
type Sample struct {
	Before     string   `json:"before"`
	Detected   int      `json:"detected"`
	Undetected int      `json:"undetected"`
	Score      *float64 `json:"score"`
}

// RunError is one run error, with a code that the protocol defines.
type RunError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Position is a 1-based line and a 1-based column that counts bytes of
// UTF-8.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Mutant is one mutant and its verdict. Tests and Seconds are present for a
// mutant that ran. CoveredBy lists the tests whose run alone executed the
// mutant's site, and is nil where no test did, or where a test binary that
// executed the site did not run each of its tests alone. Confirmed is true
// for a mutant whose Verdict, Tests and Seconds come from the run of its
// ordinary build: a survivor of its run, or a mutant without coverage.
type Mutant struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	File        string   `json:"file"`
	Scope       string   `json:"scope"`
	Start       Position `json:"start"`
	End         Position `json:"end"`
	Original    string   `json:"original"`
	Replacement string   `json:"replacement"`
	Verdict     string   `json:"verdict"`
	Tests       []string `json:"tests,omitempty"`
	CoveredBy   []string `json:"coveredBy,omitempty"`
	Seconds     *float64 `json:"seconds,omitempty"`
	Confirmed   bool     `json:"confirmed,omitempty"`
	Rule        string   `json:"rule,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// Skip is one site of a catalogue class without a mutant, and the reason.
type Skip struct {
	File   string   `json:"file"`
	Start  Position `json:"start"`
	End    Position `json:"end"`
	Reason string   `json:"reason"`
}

// Generated is a generated file of the package that the run leaves out,
// and the number of mutants that the catalogue's kinds make at its sites.
type Generated struct {
	File    string `json:"file"`
	Mutants int    `json:"mutants"`
}

// Failed reports whether the run fails: it has a run error, or a mutant
// whose verdict is not-run or error.
func (r *Record) Failed() bool {
	if len(r.Errors) > 0 {
		return true
	}
	for _, m := range r.Mutants {
		if m.Verdict == NotRun || m.Verdict == Error {
			return true
		}
	}
	return false
}

// SetScore sets Score to the detected mutants over the detected and the
// undetected ones, as protocol classifies each verdict. The score is nil
// for a run that fails, and for a run in which the score would count zero
// mutants.
//
// SetScore also sets Sample for a run without a run error in which a
// mutant is not-run: the mutants whose keys sort before the least key of a
// not-run mutant, counted as the score counts them. Sample is nil for any
// other run.
func (r *Record) SetScore(protocol spec.Protocol) {
	detected, undetected := r.tally(protocol, "")
	r.Score = ratio(detected, undetected)
	if r.Failed() {
		r.Score = nil
	}
	r.Sample = nil
	before := ""
	for _, m := range r.Mutants {
		if m.Verdict == NotRun && (before == "" || m.Key < before) {
			before = m.Key
		}
	}
	if before == "" || len(r.Errors) > 0 {
		return
	}
	detected, undetected = r.tally(protocol, before)
	r.Sample = &Sample{Before: before, Detected: detected, Undetected: undetected, Score: ratio(detected, undetected)}
}

// ratio returns detected over detected plus undetected, or nil when both
// are zero.
func ratio(detected, undetected int) *float64 {
	if detected+undetected == 0 {
		return nil
	}
	score := float64(detected) / float64(detected+undetected)
	return &score
}

// tally returns the number of detected and of undetected mutants, as
// protocol classifies each verdict, among the mutants whose keys sort
// before before, or among every mutant when before is empty.
func (r *Record) tally(protocol spec.Protocol, before string) (detected, undetected int) {
	class := map[string]string{}
	for _, v := range protocol.Verdicts {
		class[v.ID] = v.Score
	}
	for _, m := range r.Mutants {
		if before != "" && m.Key >= before {
			continue
		}
		switch class[m.Verdict] {
		case "detected":
			detected++
		case "undetected":
			undetected++
		}
	}
	return detected, undetected
}

// Line returns the line that reports m at its position in the file at
// path: path:line:column: verdict: original became replacement (kind). A
// mutant with an empty replacement reads original removed, and the line of
// a mutant whose run ended in an error ends with the reason.
func (m Mutant) Line(path string) string {
	change := m.Original + " became " + m.Replacement
	if m.Replacement == "" {
		change = m.Original + " removed"
	}
	line := fmt.Sprintf("%s:%d:%d: %s: %s (%s)", path, m.Start.Line, m.Start.Column, phrase(m.Verdict), change, m.Kind)
	if m.Verdict == Error && m.Reason != "" {
		line += ": " + m.Reason
	}
	return line
}

// phrase returns a verdict as text writes it: not covered for no-coverage,
// and the verdict with spaces in place of its hyphens for any other.
func phrase(verdict string) string {
	if verdict == NoCoverage {
		return "not covered"
	}
	return strings.ReplaceAll(verdict, "-", " ")
}

// Path returns the path of file, a file of the record, relative to dir,
// with the symbolic links of dir resolved, as an editor resolves a path
// that a test prints from dir.
func (r *Record) Path(file, dir string) string {
	dir, _ = filepath.Abs(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	rel, _ := filepath.Rel(dir, filepath.Join(r.Root, filepath.FromSlash(file)))
	return rel
}

// Summary returns one line that states the run:
//
//   - the target
//   - how many of the mutants that the score counts the tests detected, as
//     a count and as a percentage rounded down, or that the run failed, with
//     the detected mutants of a sample that is not empty
//   - the count of each verdict, in protocol's order
//   - the number of generated files that the run leaves out, and of their
//     mutants, when it leaves one out
func (r *Record) Summary(protocol spec.Protocol) string {
	counts := map[string]int{}
	for _, m := range r.Mutants {
		counts[m.Verdict]++
	}
	var parts []string
	for _, v := range protocol.Verdicts {
		if counts[v.ID] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[v.ID], phrase(v.ID)))
		}
	}
	if len(r.Generated) > 0 {
		mutants := 0
		for _, g := range r.Generated {
			mutants += g.Mutants
		}
		files, noun := "files", "mutants"
		if len(r.Generated) == 1 {
			files = "file"
		}
		if mutants == 1 {
			noun = "mutant"
		}
		part := fmt.Sprintf("%d generated %s with %d %s left out", len(r.Generated), files, mutants, noun)
		parts = append(parts, part)
	}
	detected, undetected := r.tally(protocol, "")
	head := "no mutant to detect"
	switch {
	case r.Failed() && r.Sample != nil && r.Sample.Score != nil:
		head = "the run failed, " + detection(r.Sample.Detected, r.Sample.Undetected, "of a sample of ")
	case r.Failed():
		head = "the run failed"
	case detected+undetected > 0:
		head = detection(detected, undetected, "of ")
	}
	text := r.Target.Name + ": " + head
	if len(parts) > 0 {
		text += ": " + strings.Join(parts, ", ")
	}
	return text
}

// detection states the detected mutants among the detected and the
// undetected ones, and the percentage rounded down, as in "12 of 13 mutants
// detected (92%)". of is the words before the count of mutants. The count
// is at least 1.
func detection(detected, undetected int, of string) string {
	counted := detected + undetected
	noun := "mutants"
	if counted == 1 {
		noun = "mutant"
	}
	return fmt.Sprintf("%d %s%d %s detected (%d%%)", detected, of, counted, noun, detected*100/counted)
}

// FileName returns the name of the record file of the target importPath:
// the import path escaped as a path segment, and .mutate.json.
func FileName(importPath string) string {
	return url.PathEscape(importPath) + ".mutate.json"
}

// Write writes r into dir, which Write creates when it does not exist, as
// the file that FileName names, and returns the file's path.
func (r *Record) Write(dir string) (string, error) {
	// A record contains strings, integers and finite numbers, which
	// encoding/json encodes without an error.
	data, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}
	path := filepath.Join(dir, FileName(r.Target.Name))
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}
	return path, nil
}

// EngineVersion returns the version of the engine's module in info: the
// main module's, or the dependency's, or the version of the dependency's
// replacement. It returns (devel) for a nil info, for a replacement by a
// directory, and for a binary that does not contain the module.
func EngineVersion(info *debug.BuildInfo) string {
	if info == nil {
		return "(devel)"
	}
	if info.Main.Path == Module {
		return info.Main.Version
	}
	for _, d := range info.Deps {
		if d.Path != Module {
			continue
		}
		if d.Replace != nil {
			if d.Replace.Version == "" {
				return "(devel)"
			}
			return d.Replace.Version
		}
		return d.Version
	}
	return "(devel)"
}
