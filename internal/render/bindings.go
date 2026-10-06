// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package render

import (
	"go/token"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/enumerate"
)

// The constants true and false as the instrumentation writes them:
// expressions of literals, which no declaration of the program can hide, as
// a local variable named false or a package's constant named true hides the
// predeclared identifier.
const (
	trueExpr  = "(0 == 0)"
	falseExpr = "(0 != 0)"
)

// zeroVar starts the name of each variable that a Zero form returns,
// followed by the index of the result. named writes the package's prefix in
// place of its own.
const zeroVar = namePrefix + "Zero"

// edit replaces the bytes from start to end of a file's text with text. An
// edit whose start is its end inserts text before the byte at start. Edits
// of one file do not overlap.
type edit struct {
	start, end int
	text       string
}

// zeroBindings returns the edits that give each result of fn a variable
// that contains the result's zero value wherever fn's body runs. The names
// of the variables start with the package's prefix, so no identifier of the
// package is one of them. It returns the edits in the order of their
// offsets, and the variables' names in the order of the results. A Zero
// form returns these variables. No declaration at the site changes them, as
// a local variable named nil or after the result's type changes a zero
// value written as code.
//
//   - Results without names get the names, as func() T becomes
//     func() (_mutateZero0 T). The program cannot name such a variable, and
//     only a return assigns it. A return ends the body.
//   - A result named _ gets the name in place of _.
//   - fn copies each other named result into its variable before its first
//     statement, where the result is still the zero value.
//
// zeroBindings allocates the edits and the names.
func zeroBindings(fset *token.FileSet, fn *enumerate.Function, prefix string) ([]edit, []string) {
	off := func(pos token.Pos) int { return fset.File(pos).Offset(pos) }
	zero := named(prefix, zeroVar)
	results := fn.Type.Results
	var edits []edit
	var vars, copies, sources []string
	for _, field := range results.List {
		if len(field.Names) == 0 {
			v := zero + strconv.Itoa(len(vars))
			at := off(field.Type.Pos())
			if results.Opening.IsValid() {
				edits = append(edits, edit{start: at, end: at, text: v + " "})
			} else {
				end := off(field.Type.End())
				edits = append(
					edits,
					edit{start: at, end: at, text: "(" + v + " "},
					edit{start: end, end: end, text: ")"},
				)
			}
			vars = append(vars, v)
			continue
		}
		for _, name := range field.Names {
			v := zero + strconv.Itoa(len(vars))
			if name.Name == "_" {
				edits = append(edits, edit{start: off(name.Pos()), end: off(name.End()), text: v})
			} else {
				copies, sources = append(copies, v), append(sources, name.Name)
			}
			vars = append(vars, v)
		}
	}
	if len(copies) > 0 {
		at := off(fn.Body.Lbrace) + 1
		text := " " + strings.Join(copies, ", ") + " := " + strings.Join(sources, ", ") + ";"
		edits = append(edits, edit{start: at, end: at, text: text})
	}
	return edits, vars
}
