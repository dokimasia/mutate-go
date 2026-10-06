// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec

import (
	"embed"
	"encoding/json"
	"strings"
)

// files contains the copies of the vendored definition's files that the
// engine reads when it runs.
//
//go:embed VERSION catalogue.json protocol.json overlay.json
var files embed.FS

// Definition is the part of the vendored definition that the engine reads
// when it runs.
type Definition struct {
	// Version is the catalogue's version, which every record states.
	Version   string
	Catalogue Catalogue
	Protocol  Protocol
	Overlay   Overlay
}

// Catalogue is the operator catalogue: the classes, the kinds, the rule
// families, the prefix of the key, the name of the annotation, and the
// directive that makes a generated file a target.
type Catalogue struct {
	Key        string   `json:"key"`
	Annotation string   `json:"annotation"`
	Include    string   `json:"include"`
	Classes    []Class  `json:"classes"`
	Kinds      []Kind   `json:"kinds"`
	Families   []Family `json:"families"`
}

// Class is one operator class and its kinds, in catalogue order.
type Class struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Kinds []string `json:"kinds"`
}

// Kind is one kind of mutant and its class.
type Kind struct {
	ID          string `json:"id"`
	Class       string `json:"class"`
	Description string `json:"description"`
}

// Family is one rule family of code whose mutants are suppressed.
type Family struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

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
	Instrumented string    `json:"instrumented"`
	Verdicts     []Verdict `json:"verdicts"`
	Errors       []string  `json:"errors"`
	Limits       Limits    `json:"limits"`
}

// RecordName names the record's format and the version of its layout.
type RecordName struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Verdict is one verdict and its place in the score: detected,
// undetected or excluded.
type Verdict struct {
	ID    string `json:"id"`
	Score string `json:"score"`
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

// Overlay is the Go overlay: how the catalogue maps onto Go, and the
// overlay's version, which every record states beside the catalogue's.
// Variables states which calls of a variable a result rule puts into its
// family, and Compound which compound statements a call family suppresses
// as a whole.
type Overlay struct {
	Language  string              `json:"language"`
	Version   string              `json:"version"`
	Comment   string              `json:"comment"`
	Generated string              `json:"generated"`
	Files     []string            `json:"files"`
	Scopes    []string            `json:"scopes"`
	Tokens    string              `json:"tokens"`
	Zero      string              `json:"zero"`
	Kinds     map[string][]string `json:"kinds"`
	Skips     []Skip              `json:"skips"`
	Families  Families            `json:"families"`
	Variables []string            `json:"variables"`
	Compound  []string            `json:"compound"`
}

// Skip is one reason that the engine lists a site in the record's skipped
// entries instead of making its mutants, and where the reason applies.
type Skip struct {
	Reason string `json:"reason"`
	Where  string `json:"where"`
}

// Families lists, per rule family, the APIs whose calls and arguments are
// suppressed. A call family names each function or method as
// [go/types.Func.FullName] spells it. Methods lists the methods that belong
// to a family by their name and signature, on any type of one kind, and
// Results the rules that put the calls of a variable into a family by the
// values that the variable gets.
type Families struct {
	Logging  []string   `json:"logging"`
	Timing   []string   `json:"timing"`
	Flags    []string   `json:"flags"`
	Capacity []Capacity `json:"capacity"`
	Helper   []string   `json:"helper"`
	Methods  []Method   `json:"methods"`
	Results  []Result   `json:"results"`
}

// Calls returns the call families by name: logging, timing, flags and
// helper.
func (f Families) Calls() map[string][]string {
	return map[string][]string{"logging": f.Logging, "timing": f.Timing, "flags": f.Flags, "helper": f.Helper}
}

// Method puts a call of the method Name into Family when the method's
// signature, without the receiver and the parameter names, is Signature as
// go/types writes it, and the receiver's type is of the kind On. The only
// kind is interface.
type Method struct {
	Family    string `json:"family"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
	On        string `json:"on"`
}

// Result puts a call of a variable into Family when every value that the
// variable gets is a result of the type Type of a call of a function or
// method that the family lists, as the overlay's Variables state. Type is
// written as [go/types.TypeString] writes a type without a qualifier, such
// as context.CancelFunc.
type Result struct {
	Family string `json:"family"`
	Type   string `json:"type"`
}

// Capacity is one argument that only sizes an allocation. Func is a
// function's full name, or the builtin make, whose Of names the allocated
// kind: slice or map. Argument is the argument's index, from 0.
type Capacity struct {
	Func     string `json:"func"`
	Of       string `json:"of,omitempty"`
	Argument int    `json:"argument"`
}

// Load returns the definition that the engine reads when it runs.
//
// The embed directive includes every file that Load reads, and the tests
// of this package decode each file strictly, so Load ignores the errors of
// the read and the decode.
func Load() Definition {
	var d Definition
	d.Version = strings.TrimSpace(string(read("VERSION")))
	_ = json.Unmarshal(read("catalogue.json"), &d.Catalogue)
	_ = json.Unmarshal(read("protocol.json"), &d.Protocol)
	_ = json.Unmarshal(read("overlay.json"), &d.Overlay)
	return d
}

func read(name string) []byte {
	data, _ := files.ReadFile(name)
	return data
}
