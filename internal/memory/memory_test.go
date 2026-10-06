// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package memory_test

import (
	"os"
	"testing"
	"testing/fstest"

	"go.dokimi.dev/assert"

	"go.dokimi.dev/mutate/internal/memory"
)

// The files of the fixtures: the machine's 16 MiB, and the process's cgroup
// v2, run.scope in user.slice.
const (
	meminfo  = "MemTotal:       16384 kB\nMemFree:         1024 kB\n"
	cgroup   = "0::/user.slice/run.scope\n"
	machine  = 16384 << 10
	statmPID = 42
)

// files returns a file system of the files by path.
func files(texts map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, text := range texts {
		fsys[name] = &fstest.MapFile{Data: []byte(text)}
	}
	return fsys
}

func TestMemory(t *testing.T) {
	t.Parallel()

	t.Run("Limit", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			files map[string]string
			want  int64
		}{
			{
				name: "returns the least memory.max of the cgroup and its ancestors",
				files: map[string]string{
					"proc/meminfo":     meminfo,
					"proc/self/cgroup": cgroup,
					"sys/fs/cgroup/user.slice/run.scope/memory.max": "12582912\n",
					"sys/fs/cgroup/user.slice/memory.max":           "8388608\n",
					"sys/fs/cgroup/memory.max":                      "max\n",
				},
				want: 8388608,
			},
			{
				name: "returns the machine's memory when it is less than every cgroup limit",
				files: map[string]string{
					"proc/meminfo":                        meminfo,
					"proc/self/cgroup":                    cgroup,
					"sys/fs/cgroup/user.slice/memory.max": "33554432\n",
				},
				want: machine,
			},
			{
				name: "returns the machine's memory when no cgroup states a limit",
				files: map[string]string{
					"proc/meminfo":     meminfo,
					"proc/self/cgroup": "1:name=systemd:/x\n0::/user.slice/run.scope\n",
					"sys/fs/cgroup/user.slice/run.scope/memory.max": "max\n",
				},
				want: machine,
			},
			{
				name:  "returns the cgroup's limit when meminfo does not read",
				files: map[string]string{"proc/self/cgroup": "0::/\n", "sys/fs/cgroup/memory.max": "4096\n"},
				want:  4096,
			},
			{
				name:  "returns the machine's memory when the process's cgroup does not read",
				files: map[string]string{"proc/meminfo": meminfo},
				want:  machine,
			},
			{
				name:  "returns 0 when no file reads",
				files: map[string]string{},
				want:  0,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(
					t,
					memory.Limit(files(tt.files)),
					tt.want,
					"Limit returns the least limit that the files state",
				)
			})
		}
	})

	t.Run("Resident", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the resident pages of statm times the page size", func(t *testing.T) {
			t.Parallel()
			fsys := files(map[string]string{"proc/42/statm": "100 25 10 1 0 9 0\n"})
			assert.Equal(t, memory.Resident(fsys, statmPID), 25*int64(os.Getpagesize()),
				"the second field counts the resident pages")
		})

		t.Run("returns 0 for a process whose statm does not read", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, memory.Resident(files(nil), statmPID), 0, "a process that exited uses no memory")
		})

		t.Run("returns 0 for a statm without the resident pages", func(t *testing.T) {
			t.Parallel()
			fsys := files(map[string]string{"proc/42/statm": "100\n"})
			assert.Equal(t, memory.Resident(fsys, statmPID), 0, "a statm of one field states no resident set")
		})
	})
}
