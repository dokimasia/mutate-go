// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// procAndSys writes files, by slash-separated path, into a new directory,
// and returns the directory's proc and sys, the mount points that
// memoryLimit reads.
func procAndSys(t *testing.T, files map[string]string) (proc, sys string) {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(root, "proc"), filepath.Join(root, "sys")
}

func TestBudgetLinux(t *testing.T) {
	t.Parallel()
	t.Run("memoryLimit", func(t *testing.T) {
		t.Parallel()
		meminfo := "MemTotal:       16384 kB\nMemFree:         1024 kB\n"
		cgroup := "0::/user.slice/run.scope\n"
		tests := []struct {
			name  string
			files map[string]string
			want  int64
		}{
			{
				"returns the least memory.max of the cgroup and its ancestors",
				map[string]string{
					"proc/meminfo":     meminfo,
					"proc/self/cgroup": cgroup,
					"sys/fs/cgroup/user.slice/run.scope/memory.max": "12582912\n",
					"sys/fs/cgroup/user.slice/memory.max":           "8388608\n",
					"sys/fs/cgroup/memory.max":                      "max\n",
				},
				8388608,
			},
			{
				"returns the machine's memory when it is less than every cgroup limit",
				map[string]string{
					"proc/meminfo":                        meminfo,
					"proc/self/cgroup":                    cgroup,
					"sys/fs/cgroup/user.slice/memory.max": "33554432\n",
				},
				16384 << 10,
			},
			{
				"returns the machine's memory when no cgroup states a limit",
				map[string]string{
					"proc/meminfo":     meminfo,
					"proc/self/cgroup": "1:name=systemd:/x\n0::/user.slice/run.scope\n",
					"sys/fs/cgroup/user.slice/run.scope/memory.max": "max\n",
				},
				16384 << 10,
			},
			{
				"returns the cgroup's limit when meminfo does not read",
				map[string]string{"proc/self/cgroup": "0::/\n", "sys/fs/cgroup/memory.max": "4096\n"},
				4096,
			},
			{
				"returns the machine's memory when the process's cgroup does not read",
				map[string]string{"proc/meminfo": meminfo},
				16384 << 10,
			},
			{"returns 0 when nothing reads", map[string]string{}, 0},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if got := memoryLimit(procAndSys(t, tt.files)); got != tt.want {
					t.Errorf("memoryLimit() = %d, want %d", got, tt.want)
				}
			})
		}
	})
}
