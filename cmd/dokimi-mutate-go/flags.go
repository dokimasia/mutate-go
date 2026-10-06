// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"go.dokimi.dev/mutate/internal/selection"
)

// The names of the command's flags.
const (
	flagLines            = "lines"
	flagDiff             = "diff"
	flagSample           = "sample"
	flagIncludeGenerated = "include-generated"
	flagSuite            = "suite"
	flagConfirm          = "confirm"
	flagDir              = "C"
	flagParallel         = "p"
	flagWorkers          = "workers"
	flagTimeout          = "timeout"
	flagBudget           = "memory-budget"
	flagJSON             = "json"
	flagRecord           = "record"
	flagList             = "list"
	flagVersion          = "version"
	flagHelp             = "help"
)

// The titles of the sections of the help that list flags.
const (
	selectionSection = "Selection"
	testsSection     = "Tests"
	executionSection = "Execution"
	outputSection    = "Output"
)

// currentPackage is the pattern of the package in the current directory,
// which the command tests when the command line names none.
const currentPackage = "."

// stdinPath is the file of -diff that reads standard input.
const stdinPath = "-"

// options are the settings that a command line states.
type options struct {
	dir              string
	lines            linesFlag
	diff             string
	sample           int
	includeGenerated bool
	suite            suiteFlag
	confirm          bool
	parallel         int
	workers          int
	timeout          time.Duration
	budget           sizeFlag
	json             bool
	records          string
	list             bool
	version          bool
	// patterns are the patterns of the packages to test, currentPackage
	// when the command line names none.
	patterns []string
}

// option is one flag of the command line. The table of the flags is the
// one description of each flag: it defines the flags, and the help lists
// them.
type option struct {
	// name is the flag's name, and value the placeholder of its value in
	// the help, empty for a flag that takes no value.
	name, value string
	// section is the title of the help's section that lists the flag. The
	// table lists the flags of one section together.
	section string
	// usage is the flag's description, one paragraph that the help wraps.
	usage string
	// define defines the flag on fs, with its destination in o and its
	// default from o.
	define func(fs *flag.FlagSet, name string, o *options)
}

// commandFlags is the table of the command's flags, in the help's order.
var commandFlags = []option{
	{
		name: flagLines, value: "file:first-last", section: selectionSection,
		usage: "Test only the mutants on lines first to last of file. The path is relative to the current " +
			"directory. The flag repeats.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.Var(&o.lines, name, "") },
	},
	{
		name: flagDiff, value: "file", section: selectionSection,
		usage: "Test only the mutants on the lines that the unified diff in file adds, and on the lines on " +
			"either side of each run of removed lines. A path after its b/ prefix is relative to the current " +
			"directory. The file " + stdinPath + " reads standard input.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.StringVar(&o.diff, name, o.diff, "") },
	},
	{
		name: flagSample, value: "n", section: selectionSection,
		usage: "Test only the first n mutants of each package, in the order of their keys. The same code gives " +
			"the same sample on every machine, and the score is the score of the sample.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.IntVar(&o.sample, name, o.sample, "") },
	},
	{
		name: flagIncludeGenerated, section: selectionSection,
		usage: "Also mutate generated files. A generated file has a comment line such as \"// Code generated " +
			"by stringer. DO NOT EDIT.\" before its package clause. Without the flag, the command mutates such " +
			"a file only when the comments before its package clause contain the line " + includeDirective + ".",
		define: func(fs *flag.FlagSet, name string, o *options) {
			fs.BoolVar(&o.includeGenerated, name, o.includeGenerated, "")
		},
	},
	{
		name: flagSuite, value: "pattern", section: testsSection,
		usage: "Also run the tests of the packages that pattern names against each package's mutants, where " +
			"those tests import the package. The flag repeats.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.Var(&o.suite, name, "") },
	},
	{
		name: flagConfirm, section: testsSection,
		usage: "Test each mutant that survived or that no test covers once more, in a normal build that " +
			"contains this mutant alone. Tests that skip in the instrumented build, such as allocation checks, " +
			"then run against it. Each such mutant costs one more build.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.BoolVar(&o.confirm, name, o.confirm, "") },
	},
	{
		name: flagDir, value: "dir", section: executionSection,
		usage:  "Change to dir before the command resolves packages and paths.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.StringVar(&o.dir, name, o.dir, "") },
	},
	{
		name: flagParallel, value: "n", section: executionSection,
		usage: "The number of packages to test at once. The default is 1. The packages that run at once share " +
			"the command's GOMAXPROCS threads.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.IntVar(&o.parallel, name, o.parallel, "") },
	},
	{
		name: flagWorkers, value: "n", section: executionSection,
		usage: "The number of mutants of one package to test at once. The default is 1. Above 1, tests that " +
			"share a resource, such as a fixed port, can fail each other, and each such failure counts as a " +
			"detection.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.IntVar(&o.workers, name, o.workers, "") },
	},
	{
		name: flagTimeout, value: "d", section: executionSection,
		usage: "The time limit of the command, such as 30m. A mutant starts only when it can finish before the " +
			"limit. The mutants that do not start fail the run, and the summary states the score of those " +
			"that ran. The default, 0, sets no limit.",
		define: func(fs *flag.FlagSet, name string, o *options) {
			fs.DurationVar(&o.timeout, name, o.timeout, "")
		},
	},
	{
		name: flagBudget, value: "size", section: executionSection,
		usage: "The memory that the packages tested at once may use, such as 16G, with K, M, G or T for powers " +
			"of 1024. A package starts its mutants when its workers' memory ceilings fit in the budget beside " +
			"those of the running packages. The default is three quarters of the memory that the process may " +
			"use, by its cgroup or by the machine. 0 sets no budget.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.Var(&o.budget, name, "") },
	},
	{
		name: flagJSON, section: outputSection,
		usage: "Write each package's record to standard output as one line of JSON, in place of the " +
			"text.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.BoolVar(&o.json, name, o.json, "") },
	},
	{
		name: flagRecord, value: "dir", section: outputSection,
		usage:  "Write each package's record to a file in dir.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.StringVar(&o.records, name, o.records, "") },
	},
	{
		name: flagList, section: outputSection,
		usage:  "Print the mutants of each package, and run no test.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.BoolVar(&o.list, name, o.list, "") },
	},
	{
		name: flagVersion, section: outputSection,
		usage: "Print the versions of the engine, the operator catalogue, the Go overlay and the Go toolchain " +
			"that built the engine.",
		define: func(fs *flag.FlagSet, name string, o *options) { fs.BoolVar(&o.version, name, o.version, "") },
	},
}

// parse returns the options of the command line args, with budget as the
// default of -memory-budget, -p and -workers at 1, and currentPackage as
// the pattern when args name none.
//
// Error modes:
//   - flag.ErrHelp for -h and -help
//   - the error of the flag package for a flag that is not defined or a
//     value that does not parse
//   - an error for a number below its range: -p or -workers below 1, and
//     -sample or -timeout below 0
//   - an error for -list beside -json or -record, which write a run's
//     record
func parse(args []string, budget int64) (*options, error) {
	o := &options{parallel: 1, workers: 1, budget: sizeFlag(budget)}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, opt := range commandFlags {
		opt.define(fs, opt.name, o)
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	o.patterns = fs.Args()
	if len(o.patterns) == 0 {
		o.patterns = []string{currentPackage}
	}
	switch {
	case o.parallel < 1:
		return nil, fmt.Errorf("-%s takes a number of at least 1", flagParallel)
	case o.workers < 1:
		return nil, fmt.Errorf("-%s takes a number of at least 1", flagWorkers)
	case o.sample < 0:
		return nil, fmt.Errorf("-%s takes a number of at least 0", flagSample)
	case o.timeout < 0:
		return nil, fmt.Errorf("-%s takes a duration of at least 0", flagTimeout)
	case o.list && (o.json || o.records != ""):
		return nil, fmt.Errorf("-%s writes no record, so it does not take -%s or -%s", flagList, flagJSON, flagRecord)
	}
	return o, nil
}

// suiteFlag collects the patterns of -suite, which repeats.
type suiteFlag []string

// String returns the empty text, which the help does not show.
func (*suiteFlag) String() string { return "" }

// Set adds the pattern.
func (f *suiteFlag) Set(pattern string) error {
	*f = append(*f, pattern)
	return nil
}

// linesFlag collects the entries of -lines, which repeats. Each path is as
// the entry writes it, so the command resolves it after it changes to the
// directory of -C.
type linesFlag []selection.Lines

// String returns the empty text, which the help does not show.
func (*linesFlag) String() string { return "" }

// Set adds the lines of entry, as selection.ParseEntry parses it, and
// returns its error.
func (f *linesFlag) Set(entry string) error {
	l, err := selection.ParseEntry(entry)
	if err != nil {
		return err
	}
	*f = append(*f, l)
	return nil
}

// sizeFlag is the value of -memory-budget: a number of bytes, written as a
// number with the suffix K, M, G or T for a power of 1024, or without one.
type sizeFlag int64

// units maps the suffix of a size to the power of 1024 that it states.
var units = map[byte]int64{'K': 1 << 10, 'M': 1 << 20, 'G': 1 << 30, 'T': 1 << 40}

// String returns the size in bytes.
func (f *sizeFlag) String() string { return strconv.FormatInt(int64(*f), 10) }

// Set parses s. It returns an error for a value that is not a number of at
// least 0 with an optional suffix, and for one beyond the largest int64.
func (f *sizeFlag) Set(s string) error {
	unit, digits := int64(1), s
	if n := len(s); n > 0 && units[s[n-1]] > 0 {
		unit, digits = units[s[n-1]], s[:n-1]
	}
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || v < 0 || v > math.MaxInt64/unit {
		return fmt.Errorf("%q is not a number of bytes with an optional K, M, G or T for a power of 1024", s)
	}
	*f = sizeFlag(v * unit)
	return nil
}
