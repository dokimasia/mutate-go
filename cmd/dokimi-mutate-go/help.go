// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"

	"go.dokimi.dev/mutate/internal/record"
)

// The widths of the help's text, in bytes: of a paragraph, and of a flag's
// description after its indentation of two tabs.
const (
	paragraphWidth = 76
	flagWidth      = 64
)

// The indentation of a flag in the help, and of the flag's description.
const (
	flagIndent  = "\t"
	usageIndent = "\t\t"
)

// tie joins two words of the help's text that no line break may separate,
// such as a number and its unit. The help writes it as a space.
const tie = "~"

// commandPath is the package path of the command.
const commandPath = record.Module + "/cmd/" + name

// gibibyte is the unit of the computed default of -memory-budget in the help.
const gibibyte = 1 << 30

// help writes the command's help to w. computed states, by flag name, a
// sentence on the default that this process computes for the flag, which
// the help appends to the flag's description. The documentation passes
// none, so it does not depend on the machine that writes it.
func help(w io.Writer, computed map[string]string) {
	var b strings.Builder
	paragraph(&b, name+" measures how well the tests of Go packages detect faults.")
	paragraph(&b, "The command changes each package's code in small, defined ways, such as a~+~b to a~-~b. Each "+
		"change is a mutant. The command runs the package's tests against each mutant, one at a time. A mutant "+
		"that no test detects is a gap in the tests. The command prints each such mutant at its file and line, "+
		"and a mutation score for each package.")
	b.WriteString("Usage:\n\n" + flagIndent + name + " [flags] [packages]\n\n")
	paragraph(&b, "The packages are patterns, as go list resolves them. The default is the package in the "+
		"current directory. The module under test needs no change:")
	b.WriteString(flagIndent + "go run " + commandPath + "@latest ./...\n")
	section := ""
	for _, opt := range commandFlags {
		if opt.section != section {
			section = opt.section
			b.WriteString("\n" + section + ":\n\n")
		}
		b.WriteString(flagIndent + "-" + opt.name)
		if opt.value != "" {
			b.WriteString(" " + opt.value)
		}
		b.WriteString("\n")
		usage := opt.usage
		if sentence := computed[opt.name]; sentence != "" {
			usage += " " + sentence
		}
		wrap(&b, usage, usageIndent, flagWidth)
	}
	b.WriteString("\n")
	paragraph(&b, "The command prints each mutant that survived or that no test covers when its verdict is "+
		"final, and a summary when a package's run ends:")
	b.WriteString(flagIndent + "wire/codec.go:41:9: survived: n + 1 became n - 1 (aor)\n" +
		flagIndent + "wire/codec.go:52:2: not covered: buf.Reset() removed (sbr-delete)\n" +
		flagIndent + "example.com/wire: 410 of 432 mutants detected (94%): 405 killed, 5 timed out, 20 survived, " +
		"2 not covered\n\n")
	paragraph(&b, "Progress lines and errors go to standard error.")
	b.WriteString("Exit status:\n\n" +
		flagIndent + "0  Every counted mutant was detected, and every run completed.\n" +
		flagIndent + "1  A package has a mutant that survived or that no test covers, in\n" +
		flagIndent + "   its sample when -" + flagSample + " ended its run.\n" +
		flagIndent + "2  The command line or an input is invalid.\n" +
		flagIndent + "3  A run failed, such as a package that does not build, tests that\n" +
		flagIndent + "   fail without a mutant, or a run that -" + flagTimeout + " ended. The command\n" +
		flagIndent + "   also exits with 3 when it cannot write to standard output.\n\n")
	paragraph(&b, fmt.Sprintf("A test can read two variables. %s is 0 in the control runs and the active "+
		"mutant's number in a mutant's run. %s is 1 in every run of the instrumented build. Skip an allocation "+
		"or timing assertion while %[2]s is set, and use -%s.",
		definition.Protocol.Variable, definition.Protocol.Instrumented, flagConfirm))
	b.WriteString("Examples:\n\n" +
		flagIndent + name + " ./...\n" +
		flagIndent + "git diff origin/main...HEAD | " + name + " -" + flagDiff + " " + stdinPath + " ./...\n" +
		flagIndent + name + " -" + flagWorkers + " 4 -" + flagTimeout + " 30m -" + flagRecord + " out ./...\n" +
		flagIndent + name + " -" + flagList + " ./wire\n")
	_, _ = io.WriteString(w, b.String())
}

// budgetDefault returns the sentence that states the default of
// -memory-budget in this process, whose default budget is bytes.
func budgetDefault(bytes int64) string {
	if bytes == 0 {
		return "Here the default is 0, because no file states the memory that the process may use."
	}
	return fmt.Sprintf("Here the default is %.1f"+tie+"GiB.", float64(bytes)/gibibyte)
}

// version writes the versions of the engine, of the operator catalogue, of
// the Go overlay and of the toolchain that built the engine to w, one on
// each line after its name.
func version(w io.Writer) {
	info, _ := debug.ReadBuildInfo()
	fmt.Fprintf(w, "%s %s\ncatalogue %s\noverlay %s\ntoolchain %s\n",
		name, record.EngineVersion(info), definition.Version, definition.Overlay.Version, runtime.Version())
}

// paragraph writes text to b as a paragraph of lines of at most
// paragraphWidth bytes, and a blank line after it.
func paragraph(b *strings.Builder, text string) {
	wrap(b, text, "", paragraphWidth)
	b.WriteString("\n")
}

// wrap writes text to b as lines of at most width bytes after indent,
// broken at its spaces, with each tie written as a space. Words that ties
// join, and a word longer than width, take a line of their own when they
// do not fit.
func wrap(b *strings.Builder, text, indent string, width int) {
	line := ""
	for word := range strings.FieldsSeq(text) {
		if line != "" && len(line)+1+len(word) > width {
			b.WriteString(indent + strings.ReplaceAll(line, tie, " ") + "\n")
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	b.WriteString(indent + strings.ReplaceAll(line, tie, " ") + "\n")
}
