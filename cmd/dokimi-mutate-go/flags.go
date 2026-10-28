// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"math"
	"strconv"
	"time"

	gocli "go.stealthscale.io/cli"

	"go.dokimi.dev/mutate/internal/selection"
)

// flagPrefix starts the name of a long flag.
const flagPrefix = "--"

// The names of the command's flags, as go.stealthscale.io/cli derives them
// from the fields of options, and the flags of the help and the version that
// the package declares.
const (
	flagLines            = "lines"
	flagDiff             = "diff"
	flagSample           = "sample"
	flagIncludeGenerated = "include-generated"
	flagSkip             = "skip"
	flagSuite            = "suite"
	flagConfirm          = "confirm"
	flagDirectory        = "directory"
	flagParallel         = "parallel"
	flagWorkers          = "workers"
	flagTimeout          = "timeout"
	flagBudget           = "memory-budget"
	flagJSON             = "json"
	flagRecord           = "record"
	flagHistory          = "history"
	flagList             = "list"
	flagVersion          = "version"
	flagHelp             = "help"
)

// stdinPath is the file of --diff that reads standard input.
const stdinPath = "-"

// The reasons of a number below the range of its option.
const (
	atLeastOne          = "want a number of at least 1"
	atLeastZero         = "want a number of at least 0"
	durationAtLeastZero = "want a duration of at least 0"
)

// options are the options of the command line. go.stealthscale.io/cli
// declares each field as a flag and an environment variable, such as
// --workers and DOKIMI_MUTATE_GO_WORKERS, and Packages as the positional
// arguments. The help lists the text of each field.
type options struct {
	// MemoryBudget is nil until a flag or a variable sets it, and a nil
	// budget is the default budget of the process.
	MemoryBudget *size           `help:"Memory that the packages tested at once may use, such as 16G."`
	Diff         gocli.InputFile `help:"Unified diff of the lines to test, or - for standard input."`
	Directory    string          `help:"Directory to change to before the command resolves paths." short:"C"`
	Record       string          `help:"Directory that receives the record of each package."`
	History      string          `help:"Directory of the earlier records, whose verdicts the runs reuse."`
	// Lines are the entries as each states its file, so the command resolves
	// the paths after it changes to Directory.
	Lines            []selection.Lines `help:"Lines to test, as file:first-last."`
	Skip             []string          `help:"Packages whose mutants the command does not test."`
	Suite            []string          `help:"Packages whose tests also run against the mutants."`
	Packages         []string          `help:"Patterns of the packages to test." arg:"" default:"."`
	Sample           int               `help:"Number of mutants to test in each package, the first by key."`
	Parallel         int               `help:"Number of packages that may run at once." default:"1" short:"p"`
	Workers          int               `help:"Most mutants of one package that may run at once." default:"1"`
	Timeout          time.Duration     `help:"Time limit of the command, such as 30m."`
	IncludeGenerated bool              `help:"Also mutate generated files."`
	Confirm          bool              `help:"Test each undetected mutant once more in a normal build."`
	JSON             bool              `help:"Write the record of each package as one line of JSON."`
	List             bool              `help:"List the mutants of each package instead of testing them."`
}

// problem returns the reason that the command does not run with o. source
// returns the source of the value of an option, as [gocli.Run.Source] does.
// For a number below its range, the reason contains the value and its
// source in the form of the errors of go.stealthscale.io/cli. --parallel and
// --workers take a number of at least 1, and --sample and --timeout one of
// at least 0.
// --list beside --json or --record, which write a run's record, has a
// reason of its own, and so does --history beside --lines, --diff or
// --list. problem returns the empty string for options that the command
// runs with.
func (o *options) problem(source func(field any) (gocli.Source, bool)) string {
	switch {
	case o.Parallel < 1:
		return invalid(flagParallel, strconv.Itoa(o.Parallel), source, &o.Parallel, atLeastOne)
	case o.Workers < 1:
		return invalid(flagWorkers, strconv.Itoa(o.Workers), source, &o.Workers, atLeastOne)
	case o.Sample < 0:
		return invalid(flagSample, strconv.Itoa(o.Sample), source, &o.Sample, atLeastZero)
	case o.Timeout < 0:
		return invalid(flagTimeout, o.Timeout.String(), source, &o.Timeout, durationAtLeastZero)
	case o.List && (o.JSON || o.Record != ""):
		return flagPrefix + flagList + " writes no record, so it does not take " + flagPrefix + flagJSON + " or " +
			flagPrefix + flagRecord
	case o.History != "" && (len(o.Lines) > 0 || o.Diff != "" || o.List):
		return flagPrefix + flagHistory + " gives every mutant a verdict, so it does not take " + flagPrefix +
			flagLines + ", " + flagPrefix + flagDiff + " or " + flagPrefix + flagList
	}
	return ""
}

// sizeForm is the reason that a text is no size, which
// go.stealthscale.io/cli states after the flag and its value.
const sizeForm = "want a number of bytes with an optional K, M, G or T for a power of 1024"

// size is the value of --memory-budget: a number of bytes, written as a
// number with the suffix K, M, G or T for a power of 1024, or without one.
type size int64

// units maps the suffix of a size to the power of 1024 that it states.
var units = map[byte]int64{'K': 1 << 10, 'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}

// UnmarshalText sets s to the size that text states. It returns an error for
// a text that is not a number of at least 0 with an optional suffix, and for
// a size beyond the largest int64.
func (s *size) UnmarshalText(text []byte) error {
	t := string(text)
	unit, digits := int64(1), t
	if n := len(t); n > 0 && units[t[n-1]] > 0 {
		unit, digits = units[t[n-1]], t[:n-1]
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || v < 0 || v > math.MaxInt64/unit {
		return errors.New(sizeForm) //nolint:errorprefix // the cli package states the reason after the flag
	}
	*s = size(v * unit)
	return nil
}

// invalid returns the problem of the value text of the option flag at field
// in the form of the errors of go.stealthscale.io/cli. The problem contains
// the flag, the value, the source of the value and the reason. source
// returns the source of field. For a field without a source, invalid writes
// the source as default.
func invalid(flag, text string, source func(field any) (gocli.Source, bool), field any, reason string) string {
	from, _ := source(field)
	return "invalid " + flagPrefix + flag + " " + strconv.Quote(text) + " from " + from.String() + ": " + reason
}
