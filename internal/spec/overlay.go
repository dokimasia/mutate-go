// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec

// SkipReason is the reason of a site that the engine lists as skipped
// instead of making its mutants, as the overlay spells it.
type SkipReason string

// The skip reasons of the overlay.
const (
	SkipConstant      SkipReason = "constant expression"
	SkipTypeParameter SkipReason = "operand of type-parameter type"
	SkipContextShift  SkipReason = "untyped constant in a non-constant shift"
	SkipNamedBool     SkipReason = "named boolean result"
	SkipSideEffects   SkipReason = "assignment target with side effects"
	SkipCgo           SkipReason = "file imports C"
)

// The values that the rules of a family match on.
const (
	// OnInterface is the receiver kind of a method rule that matches the
	// method of an interface. It is the only kind.
	OnInterface = "interface"
	// OfSlice and OfMap are the allocated kinds of an argument rule of make.
	OfSlice = "slice"
	OfMap   = "map"
	// Make is the builtin whose argument rules name the allocated kind.
	Make = "make"
)

// Overlay is the Go overlay: how the catalogue maps onto Go, and the
// overlay's version, which every record states beside the catalogue's.
// Families states the rules of each rule family, Variables which calls of
// a variable a result rule puts into its family, and Compound which
// compound statements a family that lists calls suppresses as a whole.
type Overlay struct {
	Language  string            `json:"language"`
	Version   string            `json:"version"`
	Comment   string            `json:"comment"`
	Generated string            `json:"generated"`
	Files     []string          `json:"files"`
	Scopes    []string          `json:"scopes"`
	Tokens    string            `json:"tokens"`
	Zero      string            `json:"zero"`
	Kinds     map[Kind][]string `json:"kinds"`
	Skips     []OverlaySkip     `json:"skips"`
	Families  map[Family]Rules  `json:"families"`
	Variables []string          `json:"variables"`
	Compound  []string          `json:"compound"`
}

// OverlaySkip is one reason that the engine lists a site in the record's
// skipped entries instead of making its mutants, and where the reason
// applies.
type OverlaySkip struct {
	Reason SkipReason `json:"reason"`
	Where  string     `json:"where"`
}

// Rules are the rules of one rule family. APIs lists the functions and
// methods whose calls the family suppresses, each as
// [go/types.Func.FullName] spells it. Methods matches the methods of an
// interface by name and signature, Results the calls of a variable by the
// values that it receives, and Arguments the arguments that only size an
// allocation.
type Rules struct {
	APIs      []string   `json:"apis,omitempty"`
	Methods   []Method   `json:"methods,omitempty"`
	Results   []Result   `json:"results,omitempty"`
	Arguments []Argument `json:"arguments,omitempty"`
}

// Method puts a call of the method Name into its family when the method's
// signature, without the receiver and the parameter names, is Signature as
// go/types writes it, and the receiver's type is of the kind On, which is
// OnInterface.
type Method struct {
	Name      string `json:"name"`
	Signature string `json:"signature"`
	On        string `json:"on"`
}

// Result puts a call of a variable into its family when every value that
// the variable receives is a result of the type Type of a call of a
// function or method that the family lists, as the overlay's Variables
// state. Type is written as [go/types.TypeString] writes a type without a
// qualifier, such as context.CancelFunc.
type Result struct {
	Type string `json:"type"`
}

// Argument puts one argument of a call into its family. Func is a
// function's full name, or Make, whose Of names the allocated kind, OfSlice
// or OfMap. Argument is the argument's index, from 0.
type Argument struct {
	Func     string `json:"func"`
	Of       string `json:"of,omitempty"`
	Argument int    `json:"argument"`
}
