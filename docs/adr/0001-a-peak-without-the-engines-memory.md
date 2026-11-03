---
adr: 0001
title: A test binary's peak without the engine's memory
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

# ADR-0001: A test binary's peak without the engine's memory

## Status

Accepted

## Context

The memory ceiling of a test binary is 4 × the binary's peak resident memory in the opening control run + 512 MiB. The ceiling is at most the memory budget and at least the peak + 512 MiB. The engine starts as many workers as ceilings fit in the budget.

The engine took the peak as the larger of two values:

- the `ru_maxrss` of the `rusage` that `wait4` returns for the binary
- the largest sum of the resident memory of the binary's process group, which the watch reads every 25 ms

`ru_maxrss` also contains the memory of the engine. Go starts a test binary with `clone(CLONE_VFORK|CLONE_VM)`, so the binary runs in the engine's address space until it calls exec. When exec releases that address space, Linux raises the binary's `maxrss` to the high-water mark of the address space. In a probe, a Go process touched 300 MiB and then started `/bin/true`, and `wait4` reported a `ru_maxrss` of 301 MiB for `/bin/true`.

Every binary that used less memory than the engine got the engine's high-water mark as its peak. The race stage of CI showed the cost. Under the race detector, a binary that touches 64 MiB got a peak near 125 MiB and a ceiling of 1012 MiB. One worker fit in a budget of 2000 MiB where the test expected two.

`ru_maxrss` contains a spike that falls between two samples of the watch, and the samples contain only the binary's own processes. Each value alone gives a wrong peak for some binaries.

## Decision

We will take a test binary's peak from `ru_maxrss` only when it is above the high-water mark of the engine's own process by more than the counting error of Linux, and from the samples of the watch otherwise, because a `ru_maxrss` near that mark can be the mark itself.

The engine reads the mark, `VmHWM` in `/proc/self/status`, after the binary exits, and takes the peak from the samples when the status file does not read or has no mark. The mark only grows, so it is at least the mark at the binary's exec. Linux keeps each counter of resident pages per CPU, and adds a CPU's count to the total once the count is max(32, 2 × CPUs) pages or more. Linux reads the totals at exec, and adds the counts of every CPU for the status file. The mark of exec can then exceed the mark of the status file by that many pages of each of the 3 counters on each CPU, and the engine allows for that difference.

## Alternatives Considered

### The larger of `ru_maxrss` and the samples

The engine keeps the larger of the two values for every binary, as it did. This value contains every spike of the binary. It lost because every binary that uses less memory than the engine gets the engine's mark as its peak. Each ceiling and the number of workers then follow the memory of the engine, which grows with the packages and the tests that it runs.

### The samples alone

The engine drops `ru_maxrss` and takes the largest sum of the watch. This value never contains the engine's memory. It lost because the samples miss a spike shorter than 25 ms. A binary whose own peak is above the engine's mark would then get a ceiling below its spike, and the watch could end the runs of mutants that repeat the spike with `exhausted`.

### A cgroup for each run

The engine starts each test binary in a cgroup of its own and reads its `memory.peak`, which counts the run's processes alone. It lost because a process can create a cgroup only below a subtree that the system delegates to it. Neither `go test` on a developer's machine nor a CI runner provides one in general.

## Consequences

**Positive:**

- The ceiling of a binary that uses less memory than the engine no longer depends on the engine's memory, and neither does the number of workers.
- A binary whose peak is above the engine's mark keeps the exact peak of `ru_maxrss`, a short spike included.

**Negative:**

- A binary whose peak is near or below the engine's mark gets its peak from the samples, which miss a spike shorter than 25 ms. The ceiling still adds 512 MiB to the peak.
- The counting error grows with the CPUs. With pages of 4 KiB it is 1.5 MiB on 4 CPUs, 24 MiB on 32 and 864 MiB on 192. On a large machine more binaries get their peak from the samples.
- A test fixture that sets a ceiling through its peak keeps its memory for 200 ms, so that the samples contain it. Each run of such a fixture takes 200 ms longer.
- Each opening control run reads `/proc/self/status` once.

**Neutral:**

- Outside Linux the engine still does not measure a peak or apply a ceiling.

## References

| What | Where |
|---|---|
| Linux's `exec_mm_put_old`, which raises the `maxrss` of the process to the high-water mark of the address space that exec replaces | https://github.com/torvalds/linux/blob/55fbedcec622a943cb09a969aef1ddcba87ee40a/fs/exec.c |
| Linux's `get_mm_counter`, which reads the total of a counter, and `get_mm_counter_sum`, which adds the counts of every CPU | https://github.com/torvalds/linux/blob/master/include/linux/mm.h |
| Linux's batch of a per-CPU counter, max(32, 2 × CPUs) | https://github.com/torvalds/linux/blob/master/lib/percpu_counter.c |
| Go 1.27.3's `forkAndExecInChild1`, which clones with `CLONE_VFORK` and `CLONE_VM` unless the child gets a user namespace | https://github.com/golang/go/blob/go1.27.3/src/syscall/exec_linux.go |
| `VmHWM` in the status of a process | https://man7.org/linux/man-pages/man5/proc_pid_status.5.html |
| The CI run whose race stage failed | https://github.com/dokimasia/mutate-go/actions/runs/41629242445 |
