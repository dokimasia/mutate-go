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
)

// Go runs the go command with args in dir and returns its standard output.
//
// The go command is the first one that env's PATH names. It runs with env
// and with PWD set to dir, so the paths it computes from its working
// directory are the paths of dir and not of a symbolic link to it. When the
// command fails, the error contains its standard error.
func Go(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	path, err := lookGo(env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, env...), "PWD="+dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// lookGo returns the path of the first go command in the directories that
// env's PATH lists. A relative or empty directory names no command.
func lookGo(env []string) (string, error) {
	var path string
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		if key == "PATH" || runtime.GOOS == "windows" && strings.EqualFold(key, "PATH") {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, goName)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no go command in the directories of PATH %q", path)
}
