// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/load"
)

func TestGo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("Go", func(t *testing.T) {
		t.Parallel()
		t.Run("returns the go command's standard output", func(t *testing.T) {
			t.Parallel()
			out, err := load.Go(ctx, t.TempDir(), os.Environ(), "env", "GOVERSION")
			if err != nil || string(out) != runtime.Version()+"\n" {
				t.Errorf("Go(env GOVERSION) = %q, %v, want %q", out, err, runtime.Version()+"\n")
			}
		})
		t.Run("returns an error with the standard error of a failing command", func(t *testing.T) {
			t.Parallel()
			_, err := load.Go(ctx, t.TempDir(), os.Environ(), "nonexistent-command")
			if err == nil || !strings.Contains(err.Error(), "go nonexistent-command: exit status 2") ||
				!strings.Contains(err.Error(), "unknown command") {
				t.Errorf("Go(nonexistent-command) error = %v, want the exit status and the go command's message", err)
			}
		})
		t.Run("runs the go command in the directory with PWD set to it", func(t *testing.T) {
			t.Parallel()
			env, _ := fakeGo(t)
			dir := t.TempDir()
			out, err := load.Go(ctx, dir, append(env, "PWD=/elsewhere"), "pwd")
			if err != nil || string(out) != dir+"\n" {
				t.Errorf("Go(pwd) = %q, %v, want %q", out, err, dir+"\n")
			}
		})
		t.Run("runs the first go command of the absolute directories in PATH", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "go"), 0o755); err != nil {
				t.Fatal(err)
			}
			notExecutable := t.TempDir()
			write(t, notExecutable, "go", "#!/bin/sh\necho wrong\n")
			path := strings.Join(
				[]string{"", "relative", dir, notExecutable, fakeGoDir},
				string(filepath.ListSeparator),
			)
			out, err := load.Go(ctx, t.TempDir(), withPath(os.Environ(), path), "pwd")
			if err != nil || len(out) == 0 || string(out) == "wrong\n" {
				t.Errorf("Go(pwd) = %q, %v, want the output of the fake go command", out, err)
			}
		})
		t.Run("returns an error when PATH names no go command", func(t *testing.T) {
			t.Parallel()
			_, err := load.Go(ctx, t.TempDir(), withPath(os.Environ(), t.TempDir()), "version")
			if err == nil || !strings.Contains(err.Error(), "no go command in the directories of PATH") {
				t.Errorf("Go() error = %v, want one that states that PATH names no go command", err)
			}
		})
	})
}
