// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package enumerate

import (
	"crypto/sha256"
	"encoding/hex"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/spec"
)

// keyDigits is the number of the digest's hexadecimal digits that a key
// keeps.
const keyDigits = 16

// Key returns a mutant's key: the first 16 hexadecimal digits of the
// SHA-256 digest of the prefix, the file, the scope, the kind, the tokens
// and the occurrence in decimal, separated by NUL bytes.
func Key(prefix, file, scope string, kind spec.Kind, tokens string, occurrence int) string {
	fields := []string{prefix, file, scope, string(kind), tokens, strconv.Itoa(occurrence)}
	sum := sha256.Sum256([]byte(strings.Join(fields, keySeparator)))
	return hex.EncodeToString(sum[:])[:keyDigits]
}

// Tokens returns the tokens of src joined by single spaces. It leaves out
// comments and the semicolons that the scanner inserts at line ends. An
// operator is written as Go spells it, and any other token as its text.
func Tokens(src []byte) string {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		if lit == "" {
			lit = tok.String()
		}
		out = append(out, lit)
	}
	return strings.Join(out, " ")
}

// The limits of a mutant's original and replacement as a record states
// them, in characters.
const (
	// maxText is the length of each field at most.
	maxText = 120
	// lead is the number of characters that a cut field keeps before the
	// first character where the original and the replacement differ.
	lead = 40
)

// ellipsis replaces the characters that a cut field leaves out.
const ellipsis = "…"

// Cut writes a mutant's original and replacement as a record states them:
// each run of whitespace replaced by one space, and at most 120 characters
// each. When either is longer, both keep the same window, which starts 40
// characters before the first character where they differ, or at the first
// character when fewer precede it. An ellipsis replaces the characters
// before the window, and another the characters after it, so the two fields
// differ wherever the mutant changes its site. The replacement of a
// deletion is empty, so the original then keeps its first 119 characters.
func Cut(original, replacement string) (string, string) {
	o := []rune(strings.Join(strings.Fields(original), " "))
	r := []rune(strings.Join(strings.Fields(replacement), " "))
	if len(o) <= maxText && len(r) <= maxText {
		return string(o), string(r)
	}
	same := 0
	for same < len(o) && same < len(r) && o[same] == r[same] {
		same++
	}
	start := max(0, same-lead)
	return window(o, start), window(r, start)
}

// window returns the characters of text from start, with an ellipsis in
// place of the characters before start, and of those past 120 characters.
func window(text []rune, start int) string {
	head, room := "", maxText
	if start > 0 {
		head, room = ellipsis, maxText-1
	}
	if len(text)-start <= room {
		return head + string(text[start:])
	}
	return head + string(text[start:start+room-1]) + ellipsis
}
