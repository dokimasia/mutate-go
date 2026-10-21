// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/load"
)

func TestGo(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("Go", func(t *testing.T) {
		t.Parallel()

		t.Run("returns the go command's standard output", func(t *testing.T) {
			t.Parallel()
			out, err := load.Go(ctx, t.TempDir(), os.Environ(), "env", "GOVERSION")
			assert.NoError(t, err, "go env runs")
			assert.Equal(t, string(out), runtime.Version()+"\n", "the output is the toolchain's version")
		})

		t.Run("runs the go command in the directory with PWD set to it", func(t *testing.T) {
			t.Parallel()
			env, _ := fakeGo(t)
			dir := t.TempDir()
			out, err := load.Go(ctx, dir, append(env, "PWD=/elsewhere"), "pwd")
			assert.NoError(t, err, "the fake go command runs")
			assert.Equal(t, string(out), dir+"\n", "PWD is the directory and not the caller's")
		})

		t.Run("runs the first go command of the absolute directories in PATH", func(t *testing.T) {
			t.Parallel()
			// The path lists an empty directory, a relative one, a directory
			// whose go is a directory, and one whose go is no command, before
			// the fake go command. Windows has no execute bit, so a file named
			// go.exe is a command there, and the path leaves that directory out.
			dirs := []string{"", "relative", files.Workspace(t, files.Tree{goFile: files.Dir()})}
			if runtime.GOOS != "windows" {
				dirs = append(dirs, files.Workspace(t, files.Tree{goFile: files.Text("#!/bin/sh\necho wrong\n")}))
			}
			path := strings.Join(append(dirs, fakeGoDir), string(filepath.ListSeparator))
			wd := t.TempDir()
			out, err := load.Go(ctx, wd, withPath(os.Environ(), path), "pwd")
			assert.NoError(t, err, "the fake go command runs")
			assert.Equal(t, string(out), wd+"\n", "the fake go command, the first one, runs")
		})

		t.Run("returns an error with the exit status and the go command's message", func(t *testing.T) {
			t.Parallel()
			_, err := load.Go(ctx, t.TempDir(), os.Environ(), "nonexistent-command")
			assert.HasError(t, err, "an unknown go command fails")
			assert.That(t, err.Error()).
				HasPrefix("go nonexistent-command: exit status 2\n", "the error states the call and its status").
				Contains("unknown command", "and the go command's message")
		})

		t.Run("ends a go command that runs through a wrapper script when the context ends", func(t *testing.T) {
			t.Parallel()
			env, _ := fakeGo(t)
			bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			assert.CompletesWithin(t, 30*time.Second, func(context.Context) error {
				_, err := load.Go(bounded, t.TempDir(), env, "hang")
				assert.HasError(t, err, "the ended command fails")
				return nil
			}, "the wrapper and the child that keeps its output open end with the context")
		})

		t.Run("returns an error when PATH names no go command", func(t *testing.T) {
			t.Parallel()
			_, err := load.Go(ctx, t.TempDir(), withPath(os.Environ(), t.TempDir()), "version")
			assert.HasError(t, err, "a PATH without a go command fails")
			assert.HasPrefix(t, err.Error(), "no go command in the directories of PATH",
				"the error states that PATH names no go command")
		})
	})
}
