// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.dokimi.dev/assert"
)

// The module of a fixture: the name of its module file, and the module file
// that module writes where a test states none.
const (
	goMod  = "go.mod"
	module = "module fixture\n\ngo 1.21\n"
)

// The modes of the files, the directories and the commands that the tests
// write.
const (
	fileMode    = 0o644
	dirMode     = 0o755
	commandMode = 0o755
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
// files.
const (
	pathVar   = "PATH"
	fakeGoVar = "FAKE_GO"
)

// fakeScript is the fake go command. It appends its arguments to the file
// calls of the directory FAKE_GO, prints that directory's env.json for go
// env, its list.json for go list and its working directory's PWD for go
// pwd, and fails for any other command.
const fakeScript = "#!/bin/sh\n" +
	"[ -n \"$FAKE_GO\" ] && printf '%s\\n' \"$*\" >> \"$FAKE_GO/" + callsFile + "\"\n" +
	"case \"$1\" in\n" +
	"env) cat \"$FAKE_GO/" + envFile + "\" ;;\n" +
	"list) cat \"$FAKE_GO/" + listFile + "\" ;;\n" +
	"pwd) echo \"$PWD\" ;;\n" +
	"*) echo \"unexpected go $1\" >&2; exit 2 ;;\n" +
	"esac\n"

// fakeGoDir contains the fake go command.
//
// TestMain writes the command before any test starts. A file that is open
// for writing while a parallel test forks a process fails exec with
// ETXTBSY.
var fakeGoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakego")
	if err == nil {
		fakeGoDir = dir
		err = os.WriteFile(filepath.Join(dir, "go"), []byte(fakeScript), commandMode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// moduleDir writes files, by slash-separated path, into a new directory,
// with the module file module unless files has one, and returns the
// directory.
func moduleDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files[goMod]; !ok {
		files[goMod] = module
	}
	for name, text := range files {
		write(t, dir, name, text)
	}
	return dir
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
	write(t, dir, envFile, string(data))
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
	write(t, dir, listFile, b.String())
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

// write writes text into the file name, a slash-separated path under dir,
// and creates the file's directory.
func write(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	assert.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode), "the directory of "+name+" is created")
	assert.NoError(t, os.WriteFile(path, []byte(text), fileMode), name+" is written")
}
