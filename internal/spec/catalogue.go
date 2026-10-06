// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec

// Kind is the name of one kind of mutant, as the catalogue spells it.
type Kind string

// The kinds of the catalogue.
const (
	AOR         Kind = "aor"
	RORBoundary Kind = "ror-boundary"
	RORTrue     Kind = "ror-true"
	RORFalse    Kind = "ror-false"
	LCRLeft     Kind = "lcr-left"
	LCRRight    Kind = "lcr-right"
	LCRTrue     Kind = "lcr-true"
	LCRFalse    Kind = "lcr-false"
	UOIIncDec   Kind = "uoi-incdec"
	UOINot      Kind = "uoi-not"
	UOIMinus    Kind = "uoi-minus"
	SBRDelete   Kind = "sbr-delete"
	SBRZero     Kind = "sbr-zero"
)

// Class is the name of one operator class, as the catalogue spells it. An
// annotation can name a class in place of each of its kinds.
type Class string

// Family is the name of one rule family, as the catalogue spells it. The
// overlay states each family's rules, and a suppressed mutant's record
// names the family that suppresses it.
type Family string

// Catalogue is the operator catalogue: the prefix of the key, the name of
// the annotation and the keyword of an annotation that names every kind,
// the directive that makes a generated file a target, the classes, the
// kinds and the rule families.
type Catalogue struct {
	Key        string            `json:"key"`
	Annotation string            `json:"annotation"`
	Every      string            `json:"every"`
	Include    string            `json:"include"`
	Classes    []CatalogueClass  `json:"classes"`
	Kinds      []CatalogueKind   `json:"kinds"`
	Families   []CatalogueFamily `json:"families"`
}

// CatalogueClass is one operator class and its kinds, in catalogue order.
type CatalogueClass struct {
	ID    Class  `json:"id"`
	Name  string `json:"name"`
	Kinds []Kind `json:"kinds"`
}

// CatalogueKind is one kind of mutant and its class.
type CatalogueKind struct {
	ID          Kind   `json:"id"`
	Class       Class  `json:"class"`
	Description string `json:"description"`
}

// CatalogueFamily is one rule family of code whose mutants are suppressed.
type CatalogueFamily struct {
	ID          Family `json:"id"`
	Description string `json:"description"`
}
