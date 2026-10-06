// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package load_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeGoDir contains a go command that prints the file env.json of the
// directory FAKE_GO for go env, that directory's list.json for go list and
// its working directory's PWD for go pwd, and fails for any other command.
//
// TestMain writes the command before any test starts. A file that is open
// for writing while a parallel test forks a process fails exec with
// ETXTBSY.
var fakeGoDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakego")
	if err == nil {
		fakeGoDir = dir
		script := "#!/bin/sh\ncase \"$1\" in\n" +
			"env) cat \"$FAKE_GO/env.json\" ;;\n" +
			"list) cat \"$FAKE_GO/list.json\" ;;\n" +
			"pwd) echo \"$PWD\" ;;\n" +
			"*) echo \"unexpected go $1\" >&2; exit 2 ;;\nesac\n"
		err = os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o755)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// module writes files into a new directory and returns it. It adds a
// go.mod at go 1.21 for the module fixture when files has none.
func module(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = "module fixture\n\ngo 1.21\n"
	}
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// fakeGo returns the environment of the test process with the fake go
// command first in PATH and FAKE_GO set to a new directory, and that
// directory, where the test writes the fake command's files.
func fakeGo(t *testing.T) (env []string, dir string) {
	t.Helper()
	dir = t.TempDir()
	env = withPath(os.Environ(), fakeGoDir+string(filepath.ListSeparator)+os.Getenv("PATH"))
	return append(env, "FAKE_GO="+dir), dir
}

// withPath returns env with PATH replaced by path.
func withPath(env []string, path string) []string {
	out := []string{"PATH=" + path}
	for _, kv := range env {
		if len(kv) < 5 || kv[:5] != "PATH=" {
			out = append(out, kv)
		}
	}
	return out
}

// write writes text into the file name of dir.
func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
