---
adr: 0002
title: A failed test before the limits of a run
status: Accepted
date: 2026-11-03
supersedes: RFC-0001, in part
superseded-by: none
rfc: none
---

<!--
  ~ Copyright Dokimasia B.V. 2026
  ~ SPDX-License-Identifier: MIT
-->

# ADR-0002: A failed test before the limits of a run

## Status

Accepted

## Context

The engine gave a run of a test binary its verdict by the first rule that matched, in this order:

1. The engine ended the run for memory: `exhausted`.
2. The engine ended the run at the backup deadline: `timed-out`.
3. The engine ended the run at the first failed test: `killed`.
4. A signal that the engine did not send ended the run: `error`.
5. The exit status is 0: `survived`.
6. The testing package's alarm ended the run: `timed-out`.
7. Otherwise: `killed`.

The definition gives `killed` to a run in which a test failed, whatever ended the run after the failure. The caller's cancellation of a run gives `not-run`.

The definition also has the mode every killer, in which each run of a mutant goes to its end. In that mode a test that fails can be followed by a test that hangs or exhausts the memory. Under the engine's order such a run would be `timed-out` or `exhausted`. The default run of the same mutant ends at the first failure, before the hang, and is `killed`.

## Decision

We will give `killed` to a run in which a test failed before the memory ceiling, the deadline, an outside signal or the testing package's alarm ended it, because the definition orders the verdicts this way and a mutant then has one verdict in both modes.

The caller's cancellation still gives `not-run`, before the rule of a failed test. The tests of such a kill are the first leaf test that failed, or every leaf test that failed in the mode every killer.

## Alternatives Considered

### The engine's order

The engine keeps the memory ceiling, the deadline and an outside signal before a failed test. The engine then records in the verdict which limit ended the run. It lost because the engine would contradict the definition, and the verdict of a mutant would depend on the mode of its run.

## Consequences

**Positive:**

- A mutant has the same verdict in the default mode and in the mode every killer, for a suite whose tests fail in a fixed order.
- A kill whose run a limit ended after the failure lists the tests that failed.

**Negative:**

- A run that hangs or exhausts the memory after a failed test counts as `killed`, so its record does not show the hang or the memory.
- The rules no longer follow the order in which the engine ends a run. A reader of a verdict has to know that a failed test comes first.

**Neutral:**

- The rules after a failed test keep their order.

## References

| What | Where |
|---|---|
| The definition's order of the verdicts and the mode every killer | https://github.com/dokimasia/mutate-spec/blob/main/docs/rfc/0005-the-tests-that-kill-and-cover-a-mutant.md |
