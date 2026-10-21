// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"go.dokimi.dev/assert"
	"go.dokimi.dev/assert/files"

	"go.dokimi.dev/mutate/internal/process"
)

// The module of a fixture: the name of its module file, and the module file
// that module writes where a test states none.
const (
	goMod  = "go.mod"
	module = "module fixture\n\ngo 1.21\n"
)

// The modes of the fake go command, which TestMain writes before any test
// starts, and of the file of its calls.
const (
	commandMode = 0o755
	fileMode    = 0o644
)

// The files of the fake go command: what it prints for go env and for go
// list, and where it appends the arguments of each call, one call per line.
const (
	envFile   = "env.json"
	listFile  = "list.json"
	callsFile = "calls"
)

// The variables of the environment that the tests set: the directories
// where the go command is, and the directory of the fake go command's
// files. pwdVar is the working directory that the fake go command prints.
const (
	pathVar   = "PATH"
	fakeGoVar = "FAKE_GO"
	pwdVar    = "PWD"
)

// The commands of the fake go command: the go commands that it answers,
// and the command with which go hang starts a copy of it that waits.
const (
	envCommand   = "env"
	listCommand  = "list"
	pwdCommand   = "pwd"
	hangCommand  = "hang"
	sleepCommand = "fake-sleep"
)

// goFile is the file name of a go command, which Windows runs only with the
// suffix .exe.
const goFile = "go" + process.ExeSuffix

// fakeGoDir contains the fake go command. The command is a copy of the test
// binary, which TestMain runs as the fake go command because its name is
// goFile. The command appends its arguments to the file calls of the
// directory FAKE_GO.
// It writes that directory's env.json for go env, its list.json for go list
// and its PWD for go pwd. For go hang it waits on a copy of itself that
// waits a minute and keeps the output open, as a wrapper script waits on the
// go command. It fails for any other command.
//
// TestMain writes the command before any test starts. A file that is open
// for writing while a parallel test forks a process fails exec with
// ETXTBSY.
var fakeGoDir string

// TestMain runs the fake go command when the test binary's name is goFile.
// It otherwise writes the fake go command, runs the tests and removes the
// command.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == goFile {
		os.Exit(runFakeGo(os.Args[1:]))
	}
	dir, err := os.MkdirTemp("", "fakego")
	if err == nil {
		fakeGoDir = dir
		err = copySelf(filepath.Join(dir, goFile))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// copySelf copies the test binary to path, as a file that its owner may
// execute.
func copySelf(path string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, commandMode)
}

// runFakeGo runs as the fake go command with the arguments args, and
// returns its exit status.
func runFakeGo(args []string) int {
	if slices.Equal(args, []string{sleepCommand}) {
		time.Sleep(time.Minute)
		return 0
	}
	dir := os.Getenv(fakeGoVar)
	if dir != "" {
		f, err := os.OpenFile(filepath.Join(dir, callsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
		if err == nil {
			_, err = fmt.Fprintln(f, strings.Join(args, " "))
			err = errors.Join(err, f.Close())
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	var command string
	if len(args) > 0 {
		command = args[0]
	}
	switch command {
	case envCommand:
		return cat(filepath.Join(dir, envFile))
	case listCommand:
		return cat(filepath.Join(dir, listFile))
	case pwdCommand:
		fmt.Fprintln(os.Stdout, os.Getenv(pwdVar))
	case hangCommand:
		// The copy runs in the temporary directory, which Windows lets the
		// test remove while the copy runs.
		//nolint:noctx // the fake go command runs outside any test, and the call that started it ends it
		child := exec.Command(os.Args[0], sleepCommand)
		child.Stdout, child.Dir = os.Stdout, os.TempDir()
		_ = child.Run()
	default:
		fmt.Fprintf(os.Stderr, "unexpected go %s\n", command)
		return 2
	}
	return 0
}

// cat writes the file at path to the standard output, and returns 0, or 1
// with the error on the standard error when the file does not read.
func cat(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stdout.Write(data); err != nil {
		return 1
	}
	return 0
}

// moduleDir writes texts, by slash-separated path, into a workspace of the
// test, with the module file module unless texts has one, and returns the
// workspace's directory. It only reads texts, so parallel tests can share
// one map.
func moduleDir(t *testing.T, texts map[string]string) string {
	t.Helper()
	tree := files.Tree{goMod: files.Text(module)}
	for name, text := range texts {
		tree[name] = files.Text(text)
	}
	return files.Workspace(t, tree)
}

// resolved returns dir with its symbolic links resolved.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(dir)
	assert.NoError(t, err, "the directory's links resolve")
	return dir
}

// fakeGo returns the environment of the test process with the fake go
// command first in PATH and FAKE_GO set to a new directory, and that
// directory, where the test writes the fake command's files.
func fakeGo(t *testing.T) (env []string, dir string) {
	t.Helper()
	dir = t.TempDir()
	env = withPath(os.Environ(), fakeGoDir+string(filepath.ListSeparator)+os.Getenv(pathVar))
	return append(env, fakeGoVar+"="+dir), dir
}

// fakeEnv writes the env.json of a fake go command whose version is the
// engine's own.
func fakeEnv(t *testing.T, dir string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"GOVERSION": runtime.Version(), "GOMODCACHE": "/nonexistent", "GOARCH": runtime.GOARCH,
	})
	assert.NoError(t, err, "the variables encode")
	files.Write(t, dir, files.Tree{envFile: files.Text(string(data))})
}

// fakeList writes the list.json of a fake go command: the entries in the
// order given, each encoded as go list encodes it.
func fakeList(t *testing.T, dir string, entries ...map[string]any) {
	t.Helper()
	var b strings.Builder
	for _, e := range entries {
		data, err := json.Marshal(e)
		assert.NoError(t, err, "the entry encodes")
		b.Write(data)
		b.WriteByte('\n')
	}
	files.Write(t, dir, files.Tree{listFile: files.Text(b.String())})
}

// calls returns the arguments of each call of the fake go command whose
// directory is dir, one call per line.
func calls(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, callsFile))
	assert.NoError(t, err, "the fake go command recorded its calls")
	return string(data)
}

// withPath returns env with PATH replaced by path.
func withPath(env []string, path string) []string {
	out := []string{pathVar + "=" + path}
	for _, kv := range env {
		if !strings.HasPrefix(kv, pathVar+"=") {
			out = append(out, kv)
		}
	}
	return out
}
