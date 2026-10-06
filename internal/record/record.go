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

	"go.dokimi.dev/mutate/internal/spec"
)

// Module is the module path of the engine.
const Module = "go.dokimi.dev/mutate"

// fileSuffix ends the name of a record file.
const fileSuffix = ".mutate.json"

// The modes of the directory of the record files that Write makes, and of
// a record file.
const (
	dirMode  = 0o755
	fileMode = 0o644
)

// indent is the indentation of each level of a record file's JSON.
const indent = "  "

// develVersion is the version that the go command states for a module
// whose version it does not know, such as a main module built outside a
// version control checkout or a module replaced by a directory.
const develVersion = "(devel)"

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
	// Generated lists each generated file of the package without the
	// include directive, with the mutants that the kinds make in it, and
	// whether the run included it.
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
// confirms its survivors, and Closing is nil when the closing control run
// did not complete: the run ended before or during it, or it failed.
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
// sample of the target's mutants. Limit is the number of mutant runs that
// the caller allowed, when that limit alone ended the runs, and nil when
// the caller's deadline or the caller ended them. Score is nil for an empty
// sample.
type Sample struct {
	Before     string   `json:"before"`
	Limit      *int     `json:"limit"`
	Detected   int      `json:"detected"`
	Undetected int      `json:"undetected"`
	Score      *float64 `json:"score"`
}

// RunError is one run error, with a code that the protocol defines.
type RunError struct {
	Code    spec.ErrorCode `json:"code"`
	Message string         `json:"message"`
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
	Key         string       `json:"key"`
	Kind        spec.Kind    `json:"kind"`
	File        string       `json:"file"`
	Scope       string       `json:"scope"`
	Start       Position     `json:"start"`
	End         Position     `json:"end"`
	Original    string       `json:"original"`
	Replacement string       `json:"replacement"`
	Verdict     spec.Verdict `json:"verdict"`
	Tests       []string     `json:"tests,omitempty"`
	CoveredBy   []string     `json:"coveredBy,omitempty"`
	Seconds     *float64     `json:"seconds,omitempty"`
	Confirmed   bool         `json:"confirmed,omitempty"`
	Rule        spec.Family  `json:"rule,omitempty"`
	Reason      string       `json:"reason,omitempty"`
}

// Skip is one site of a catalogue class without a mutant, and the reason.
type Skip struct {
	File   string          `json:"file"`
	Start  Position        `json:"start"`
	End    Position        `json:"end"`
	Reason spec.SkipReason `json:"reason"`
}

// Generated is a generated file of the package without the include
// directive, the number of mutants that the catalogue's kinds make at its
// sites, and whether the run included the file, so that its mutants are
// among the record's mutants and the score counts them.
type Generated struct {
	File     string `json:"file"`
	Mutants  int    `json:"mutants"`
	Included bool   `json:"included"`
}

// Failed reports whether the run fails: it has a run error, a mutant whose
// verdict is error, a mutant whose verdict is not-run for another reason
// than the caller's limit on the number of mutant runs, which the sample's
// Limit states, or an opening control run that passed and a closing control
// run that did not complete. Without the closing control run, no run checked
// that the mutant runs left the tests' state intact.
func (r *Record) Failed() bool {
	if len(r.Errors) > 0 || r.Control != nil && r.Control.Closing == nil {
		return true
	}
	limited := r.Sample != nil && r.Sample.Limit != nil
	for _, m := range r.Mutants {
		if m.Verdict == spec.Error || m.Verdict == spec.NotRun && !limited {
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
// other run. limit is the number of mutant runs that the caller allowed,
// when that limit alone ended the runs, or 0. The sample then states it,
// and the run's score is the sample's.
func (r *Record) SetScore(protocol spec.Protocol, limit int) {
	detected, undetected := r.Tally(protocol, "")
	r.Score = ratio(detected, undetected)
	r.Sample = nil
	before := ""
	for _, m := range r.Mutants {
		if m.Verdict == spec.NotRun && (before == "" || m.Key < before) {
			before = m.Key
		}
	}
	if before != "" && len(r.Errors) == 0 {
		detected, undetected = r.Tally(protocol, before)
		r.Sample = &Sample{
			Before:     before,
			Detected:   detected,
			Undetected: undetected,
			Score:      ratio(detected, undetected),
		}
		if limit > 0 {
			r.Sample.Limit, r.Score = &limit, r.Sample.Score
		}
	}
	if r.Failed() {
		r.Score = nil
	}
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

// Tally returns the number of the detected and of the undetected mutants,
// as protocol classifies each verdict, among the mutants whose keys sort
// before bound, or among every mutant when bound is empty.
func (r *Record) Tally(protocol spec.Protocol, bound string) (detected, undetected int) {
	for _, m := range r.Mutants {
		if bound != "" && m.Key >= bound {
			continue
		}
		switch protocol.Class(m.Verdict) {
		case spec.Detected:
			detected++
		case spec.Undetected:
			undetected++
		case spec.Excluded:
		}
	}
	return detected, undetected
}

// Path returns the path of file, a file of the record, relative to dir, a
// directory as [Resolved] returns it. Path reads no file system.
func (r *Record) Path(file, dir string) string {
	rel, _ := filepath.Rel(dir, filepath.Join(r.Root, filepath.FromSlash(file)))
	return rel
}

// Resolved returns dir as [Record.Path] reads a directory: absolute, with
// its symbolic links resolved, as an editor resolves a path that a test
// prints from dir. A directory whose links do not resolve, such as one that
// does not exist, is only made absolute. A caller resolves its directory
// once and passes the result to each call of Path.
func Resolved(dir string) string {
	// filepath.Abs fails only without a working directory, in which no run
	// loads its package either.
	dir, _ = filepath.Abs(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// FileName returns the name of the record file of the target importPath:
// the import path escaped as a path segment, and .mutate.json.
func FileName(importPath string) string {
	return url.PathEscape(importPath) + fileSuffix
}

// Write writes r into dir, which Write creates when it does not exist, as
// the file that FileName names, and returns the file's path. The file is
// the record's JSON, indented by two spaces a level, and a newline.
//
// Error modes:
//   - the error of a directory that does not exist and cannot be made
//   - the error of a file that cannot be written
//
// Each starts with the package's name.
func (r *Record) Write(dir string) (string, error) {
	// A record contains strings, integers and finite numbers, which
	// encoding/json encodes without an error.
	data, _ := json.MarshalIndent(r, "", indent)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", fmt.Errorf("record: %w", err)
	}
	path := filepath.Join(dir, FileName(r.Target.Name))
	if err := os.WriteFile(path, append(data, '\n'), fileMode); err != nil {
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
		return develVersion
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
				return develVersion
			}
			return d.Replace.Version
		}
		return d.Version
	}
	return develVersion
}
