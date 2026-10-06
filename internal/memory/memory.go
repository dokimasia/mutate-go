// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package memory

import (
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
)

// The files of procfs and sysfs that the package reads, as paths under the
// root of the file system.
const (
	// meminfo states the machine's memory.
	meminfo = "proc/meminfo"
	// selfCgroup states the cgroups of the process that reads it.
	selfCgroup = "proc/self/cgroup"
	// cgroups is the mount point of the cgroup v2 hierarchy.
	cgroups = "sys/fs/cgroup"
	// memoryMax states the memory limit of one cgroup, or max for none.
	memoryMax = "memory.max"
	// procs is the directory of the processes' files, and statm the file
	// that states one process's memory in pages.
	procs = "proc"
	statm = "statm"
)

// The text of the lines of procfs that the package reads.
const (
	// memTotal starts the line of meminfo that states the machine's memory,
	// in the unit kiB.
	memTotal = "MemTotal:"
	kiB      = "kB"
	// unified starts the line of a process's cgroup file that names its
	// cgroup v2: the hierarchy 0, which has no controllers.
	unified = "0::"
)

// kibibyte is the size of the unit kB of meminfo, and of the peak that
// wait4 reports on Linux.
const kibibyte = 1 << 10

// Limit returns the memory that the process may use, in bytes: the
// machine's memory, MemTotal in proc/meminfo under root, or the least
// memory.max of the process's cgroup v2 and its ancestors under
// sys/fs/cgroup when that is less. A cgroup without a limit states max and
// counts as none. Limit returns 0 when no file states a limit, as on a
// platform without procfs.
func Limit(root fs.FS) int64 {
	var limit int64
	if data, err := fs.ReadFile(root, meminfo); err == nil {
		for line := range strings.Lines(string(data)) {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == memTotal && fields[2] == kiB {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				limit = kb * kibibyte
			}
		}
	}
	data, err := fs.ReadFile(root, selfCgroup)
	if err != nil {
		return limit
	}
	for line := range strings.Lines(string(data)) {
		cgroup, ok := strings.CutPrefix(strings.TrimSpace(line), unified)
		if !ok {
			continue
		}
		for dir := cgroup; ; dir = path.Dir(dir) {
			// A file that does not read states no limit, and neither does max.
			// Neither parses.
			text, _ := fs.ReadFile(root, path.Join(cgroups, dir, memoryMax))
			v, err := strconv.ParseInt(strings.TrimSpace(string(text)), 10, 64)
			if err == nil && (limit == 0 || v < limit) {
				limit = v
			}
			if dir == "/" {
				break
			}
		}
	}
	return limit
}

// Resident returns the resident memory of the process pid, in bytes: the
// resident pages that proc/<pid>/statm under root states, times the size of
// a page. It returns 0 for a process that has exited, whose file no longer
// reads, and on a platform without procfs.
func Resident(root fs.FS, pid int) int64 {
	data, _ := fs.ReadFile(root, path.Join(procs, strconv.Itoa(pid), statm))
	// The fields are the sizes of the whole program and of its resident
	// set, and then of five other sets.
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, _ := strconv.ParseInt(fields[1], 10, 64)
	return pages * int64(os.Getpagesize())
}
