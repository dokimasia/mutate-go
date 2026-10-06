// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.dokimi.dev/mutate/internal/process"
)

// The variables of the environment that Go reads and writes.
const (
	// pathVar lists the directories where Go looks for the go command.
	pathVar = "PATH"
	// pwdVar names the working directory of the go command.
	pwdVar = "PWD"
)

// windows is the GOOS of Windows, whose variable names ignore case and
// whose files have no execute bit.
const windows = "windows"

// executable is the mode of a file that a user may execute.
const executable = 0o111

// drainDelay is how long Go reads the go command's output after the
// command exited and Go ended its group. A process that the command started
// and that left the group can keep the output open.
const drainDelay = 5 * time.Second

// Go runs the go command with args in dir and returns its standard output.
//
// The go command is the first one that env's PATH names. It runs with env
// and with PWD set to dir, so the paths it computes from its working
// directory are the paths of dir and not of a symbolic link to it. It runs
// in a process group of its own, as internal/process runs a command, and
// Go ends the group when ctx ends. A go command that runs through a wrapper
// script ends with every process that the wrapper started.
//
// When the command fails, the error contains its standard error. A command
// that ctx ended fails with the signal that killed it.
func Go(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	path, err := lookGo(env)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, env...), pwdVar+"="+dir)
	var stdout, stderr bytes.Buffer
	g, err := process.Start(cmd, &stdout, &stderr)
	if err == nil {
		stop := context.AfterFunc(ctx, g.Kill)
		err = g.Wait()
		stop()
		g.Drain(drainDelay)
	}
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// lookGo returns the path of the first go command in the directories that
// env's PATH lists. A relative or empty directory names no command.
func lookGo(env []string) (string, error) {
	var path string
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		if key == pathVar || runtime.GOOS == windows && strings.EqualFold(key, pathVar) {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, goName)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && (runtime.GOOS == windows || info.Mode()&executable != 0) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no go command in the directories of PATH %q", path)
}
