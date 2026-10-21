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

// The files of procfs that a Group reads, as paths under the root of the
// file system. procs is the directory of the processes. stat is the file of
// a process that contains its process group, and statm the file of a
// process that contains its memory in pages.
const (
	procs = "proc"
	stat  = "stat"
	statm = "statm"
)

// groupField and residentField are the indexes of the fields that a Group
// reads. The fields of a stat after the command's name start with the
// state and the parent's process ID, and the process group follows them.
// The fields of a statm start with the size of the whole program, and the
// resident pages follow it.
const (
	groupField    = 2
	residentField = 1
)

// Group reads the resident memory of the processes of one process group on
// Linux.
//
// Resident keeps the group of each entry of procfs that it lists. It reads
// the stat of an entry only when the previous call did not list the entry.
// Each call reads the statm of each process of the group, and the stat of
// each process that started since the previous call. A process ID that two
// calls in a row list keeps the group that the first call read.
//
// # Concurrency
//
// A Group is not safe for concurrent use.
//
// # Allocation contract
//
// Resident allocates the listing of procfs, a map with one entry for each
// name in it, and the contents of each stat and statm that it reads.
type Group struct {
	root fs.FS
	id   int
	// members maps the name of each entry that the previous call of Resident
	// listed to true when the entry is a process of the group.
	members map[string]bool
}

// NewGroup returns the Group of the processes of the process group id,
// whose files Resident reads from procfs under root.
func NewGroup(root fs.FS, id int) *Group {
	return &Group{root: root, id: id}
}

// Resident returns the resident memory of the group's processes together,
// in bytes: the resident pages in the proc/<pid>/statm of each process
// under the root, times the size of a page. A process is in the group when
// the fifth field of its proc/<pid>/stat is the group's ID. Resident leaves
// out the entries of procfs that are not processes, because they have no
// stat. It returns 0 when procfs does not list a process of the group, as
// on a platform without procfs.
func (g *Group) Resident() int64 {
	// A directory that does not read gives an empty listing.
	entries, _ := fs.ReadDir(g.root, procs)
	listed := make(map[string]bool, len(entries))
	var pages int64
	for _, e := range entries {
		member, ok := g.members[e.Name()]
		if !ok {
			member = processGroup(g.root, e.Name()) == g.id
		}
		listed[e.Name()] = member
		if member {
			pages += residentPages(g.root, e.Name())
		}
	}
	g.members = listed
	return pages * int64(os.Getpagesize())
}

// processGroup returns the process group of the process of the entry name
// of procfs under root, or 0 when its stat does not read or ends before the
// group. The stat contains the command's name in parentheses, and the name
// can contain spaces and parentheses, so the fields after it start after the
// last parenthesis.
func processGroup(root fs.FS, name string) int {
	// A file that does not read has no fields, and a field that does not
	// parse gives 0. No process group has the ID 0.
	data, _ := fs.ReadFile(root, path.Join(procs, name, stat))
	text := string(data)
	fields := strings.Fields(text[strings.LastIndexByte(text, ')')+1:])
	if len(fields) <= groupField {
		return 0
	}
	id, _ := strconv.Atoi(fields[groupField])
	return id
}

// residentPages returns the resident pages of the process of the entry name
// of procfs under root: the second field of its statm. It returns 0 for a
// process that has exited, whose statm no longer reads.
func residentPages(root fs.FS, name string) int64 {
	// A file that does not read has no fields, and a field that does not
	// parse gives 0 pages.
	data, _ := fs.ReadFile(root, path.Join(procs, name, statm))
	fields := strings.Fields(string(data))
	if len(fields) <= residentField {
		return 0
	}
	pages, _ := strconv.ParseInt(fields[residentField], 10, 64)
	return pages
}
