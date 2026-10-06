// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// memoryLimit returns the memory that the process may use: the machine's
// memory, MemTotal in meminfo, or the least memory.max of the process's
// cgroup v2 and its ancestors when that is less. proc and sys are the mount
// points of procfs and sysfs. It returns 0 when neither states a limit.
func memoryLimit(proc, sys string) int64 {
	var limit int64
	if data, err := os.ReadFile(filepath.Join(proc, "meminfo")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == "MemTotal:" && fields[2] == "kB" {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				limit = kb << 10
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(proc, "self", "cgroup"))
	if err != nil {
		return limit
	}
	for _, line := range strings.Split(string(data), "\n") {
		path, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		for dir := path; ; dir = filepath.Dir(dir) {
			// A cgroup without a limit states max, and a file that does not
			// read states nothing. Neither parses.
			text, _ := os.ReadFile(filepath.Join(sys, "fs", "cgroup", dir, "memory.max"))
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
