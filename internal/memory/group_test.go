// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package memory_test

import (
	"os"
	"testing"
	"testing/fstest"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/memory"
)

// groupID is the ID of the fixtures' process group, the process ID of its
// leader.
const groupID = 10

// The directories of the fixtures' processes in procfs: the group's leader,
// a process of the group whose parent exited, a process of another group,
// and a process of the group that a case starts after a call.
const (
	leaderDir = "proc/10/"
	childDir  = "proc/11/"
	otherDir  = "proc/12/"
	newDir    = "proc/13/"
)

// The names of a process's files in procfs.
const (
	statFile  = "stat"
	statmFile = "statm"
)

// The stat lines of the fixtures' processes, whose fifth field is the
// process group. The child's name contains spaces and parentheses, and its
// parent is init, so the fields after the first parenthesis state another
// group. reusedStat is the stat of a process of another group that has the
// child's ID.
const (
	leaderStat = "10 (pkg.test) S 1 10 10 0 -1 4194304\n"
	childStat  = "11 (sh (x) y) S 1 10 10 0 -1 4194304\n"
	otherStat  = "12 (other) S 1 12 12 0 -1 4194304\n"
	newStat    = "13 (new) S 10 10 10 0 -1 4194304\n"
	reusedStat = "11 (reused) S 1 12 12 0 -1 4194304\n"
)

// The statm lines of the fixtures' processes, with 25, 5, 100 and 7
// resident pages in the second field.
const (
	leaderStatm = "100 25 10 1 0 9 0\n"
	childStatm  = "50 5 2 1 0 3 0\n"
	otherStatm  = "500 100 20 1 0 30 0\n"
	newStatm    = "40 7 2 1 0 3 0\n"
)

// group returns the files of procfs of the leader, the child and the
// process of another group.
func group() fstest.MapFS {
	return files(map[string]string{
		leaderDir + statFile:  leaderStat,
		leaderDir + statmFile: leaderStatm,
		childDir + statFile:   childStat,
		childDir + statmFile:  childStatm,
		otherDir + statFile:   otherStat,
		otherDir + statmFile:  otherStatm,
	})
}

func TestGroup(t *testing.T) {
	t.Parallel()
	page := int64(os.Getpagesize())

	t.Run("Resident", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the resident memory of the group's processes together", func(t *testing.T) {
			t.Parallel()
			g := memory.NewGroup(group(), groupID)
			assert.Equal(t, g.Resident(), 30*page, "the leader and the child count, and the other process does not")
		})

		t.Run("reads the group of a process whose name contains spaces and parentheses", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			delete(fsys, leaderDir+statmFile)
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 5*page, "the fields of the child's stat start after the last parenthesis")
		})

		t.Run("returns 0 without procfs", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, memory.NewGroup(files(nil), groupID).Resident(), 0, "procfs lists no process")
		})

		t.Run("leaves out a process whose stat does not read", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			delete(fsys, childDir+statFile)
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 25*page, "a process without a stat is in no group")
		})

		t.Run("leaves out a process whose stat ends before the group", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			fsys[childDir+statFile] = &fstest.MapFile{Data: []byte("11 (sh) S 1\n")}
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 25*page, "a stat without the fifth field states no group")
		})

		t.Run("counts no memory for a process whose statm does not read", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			delete(fsys, childDir+statmFile)
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 25*page, "a process that exited uses no memory")
		})

		t.Run("counts no memory for a statm without the resident pages", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			fsys[childDir+statmFile] = &fstest.MapFile{Data: []byte("50\n")}
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 25*page, "a statm of one field states no resident pages")
		})

		t.Run("keeps the group of a process that the previous call listed", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 30*page, "the first call reads the group of each process")
			fsys[childDir+statFile] = &fstest.MapFile{Data: []byte(reusedStat)}
			assert.Equal(t, g.Resident(), 30*page, "the second call does not read the child's stat again")
		})

		t.Run("reads the group of a process that started after the previous call", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 30*page, "the first call counts the leader and the child")
			fsys[newDir+statFile] = &fstest.MapFile{Data: []byte(newStat)}
			fsys[newDir+statmFile] = &fstest.MapFile{Data: []byte(newStatm)}
			assert.Equal(t, g.Resident(), 37*page, "the second call counts the new process of the group too")
		})

		t.Run("reads the group again for a process ID that the previous call did not list", func(t *testing.T) {
			t.Parallel()
			fsys := group()
			g := memory.NewGroup(fsys, groupID)
			assert.Equal(t, g.Resident(), 30*page, "the first call counts the child")
			delete(fsys, childDir+statFile)
			delete(fsys, childDir+statmFile)
			assert.Equal(t, g.Resident(), 25*page, "the second call does not list the child that exited")
			fsys[childDir+statFile] = &fstest.MapFile{Data: []byte(reusedStat)}
			fsys[childDir+statmFile] = &fstest.MapFile{Data: []byte(childStatm)}
			assert.Equal(t, g.Resident(), 25*page, "the third call reads the group of the new process with that ID")
		})
	})
}
