// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec

// Verdict is the name of a mutant's verdict, as the protocol spells it.
type Verdict string

// The verdicts of the protocol.
const (
	Killed      Verdict = "killed"
	TimedOut    Verdict = "timed-out"
	Exhausted   Verdict = "exhausted"
	Survived    Verdict = "survived"
	NoCoverage  Verdict = "no-coverage"
	NotViable   Verdict = "not-viable"
	Suppressed  Verdict = "suppressed"
	NotSelected Verdict = "not-selected"
	NotRun      Verdict = "not-run"
	Error       Verdict = "error"
)

// ScoreClass is how the score counts a verdict, as the protocol spells it.
type ScoreClass string

// The classes of the score.
const (
	// Detected counts in the score's numerator and its denominator.
	Detected ScoreClass = "detected"
	// Undetected counts in the score's denominator.
	Undetected ScoreClass = "undetected"
	// Excluded counts in neither.
	Excluded ScoreClass = "excluded"
)

// ErrorCode is the code of a run error, as the protocol spells it.
type ErrorCode string

// The run errors of the protocol.
const (
	ErrorLoad            ErrorCode = "load"
	ErrorWithoutReason   ErrorCode = "annotation-without-reason"
	ErrorStale           ErrorCode = "stale-annotation"
	ErrorBuild           ErrorCode = "build"
	ErrorControl         ErrorCode = "control-failed"
	ErrorNotInstrumented ErrorCode = "not-instrumented"
	ErrorOrdinary        ErrorCode = "ordinary-control-failed"
	ErrorClosing         ErrorCode = "closing-control-failed"
	ErrorChangedFiles    ErrorCode = "changed-files"
)

// Protocol is the run protocol: the record's name and layout version, the
// environment variables of the runs of the suite, the verdicts and how the
// score counts each, the run errors and the limits.
type Protocol struct {
	Record RecordName `json:"record"`
	// Variable is set to 0 in a control run and to the mutant's ordinal in
	// a mutant's run and in its confirmation run.
	Variable string `json:"variable"`
	// Instrumented is set to 1 in every run of the instrumented program, and
	// unset in the ordinary control run and in each confirmation run.
	Instrumented string            `json:"instrumented"`
	Verdicts     []ProtocolVerdict `json:"verdicts"`
	Errors       []ErrorCode       `json:"errors"`
	Limits       Limits            `json:"limits"`
}

// Class returns how the score counts the verdict v: the class that the
// protocol gives v, or "" for a verdict that the protocol does not define,
// such as the empty verdict of a mutant that a listing would test. Every
// count of detected or undetected mutants reads the class here. Class
// compares v with each verdict of the protocol.
func (p Protocol) Class(v Verdict) ScoreClass {
	for _, pv := range p.Verdicts {
		if pv.ID == v {
			return pv.Score
		}
	}
	return ""
}

// RecordName names the record's format and the version of its layout.
type RecordName struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// ProtocolVerdict is one verdict and how the score counts it.
type ProtocolVerdict struct {
	ID    Verdict    `json:"id"`
	Score ScoreClass `json:"score"`
}

// Limits are the deadline and the memory ceiling of one mutant's run. Each
// is a factor of the opening control run's measurement plus a constant.
type Limits struct {
	Deadline DeadlineLimit `json:"deadline"`
	Memory   MemoryLimit   `json:"memory"`
}

// DeadlineLimit is Factor times the opening control run's wall time plus
// Seconds.
type DeadlineLimit struct {
	Factor  float64 `json:"factor"`
	Seconds float64 `json:"seconds"`
}

// MemoryLimit is Factor times the opening control run's peak resident
// memory plus Bytes.
type MemoryLimit struct {
	Factor float64 `json:"factor"`
	Bytes  int64   `json:"bytes"`
}
