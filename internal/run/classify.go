// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"fmt"
	"syscall"

	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// failures states, by the verdict that classify gives a failed control run,
// what the tests that the run names did, or the test binary when it names
// none.
var failures = map[spec.Verdict]string{
	spec.Killed:    "failed",
	spec.TimedOut:  "ran until the deadline",
	spec.Exhausted: "exceeded the memory ceiling",
}

// classify gives a run its verdict by the first rule that matches: the run
// did not start; the engine ended it for memory, at the deadline, for the
// caller, or at the first failed test; a signal that the engine did not
// send ended it; it passed; the testing package's alarm ended it; and
// otherwise a test failed.
//
// The tests of a run that ended before its tests did are the tests that
// were running. The same applies to a failed run whose output names no
// failed test, such as a test that calls os.Exit(1).
func classify(res *testbin.Result) (verdict spec.Verdict, tests []string, reason string) {
	if res.Err != nil {
		return spec.Error, nil, "the run did not start: " + res.Err.Error()
	}
	status, _ := res.State.Sys().(syscall.WaitStatus)
	switch {
	case res.Ended == testbin.Memory:
		return spec.Exhausted, res.Output.Running, ""
	case res.Ended == testbin.Deadline:
		return spec.TimedOut, res.Output.Running, ""
	case res.Ended == testbin.Cancelled:
		return spec.NotRun, nil, cancelled
	case res.Ended == testbin.Failed:
		return spec.Killed, res.Output.Failed, ""
	case status.Signaled():
		reason = fmt.Sprintf("the run ended on the signal %s, which the engine did not send", status.Signal())
		return spec.Error, nil, reason
	case res.State.ExitCode() == 0:
		return spec.Survived, nil, ""
	case res.Output.TimedOut:
		return spec.TimedOut, res.Output.Running, ""
	case len(res.Output.Failed) == 0:
		return spec.Killed, res.Output.Running, ""
	}
	return spec.Killed, res.Output.Failed, ""
}
