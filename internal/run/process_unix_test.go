// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build unix

package run_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.dokimi.dev/mutate/internal/record"
	"go.dokimi.dev/mutate/internal/run"
)

// killer is a test file whose test ends its own binary with SIGKILL when
// Add is wrong, or always when FIXTURE_ALWAYS is set.
const killer = `package fixture

import (
	"os"
	"syscall"
	"testing"
)

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 || os.Getenv("FIXTURE_ALWAYS") != "" {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}
}
`

// blocker is a test file whose test writes a byte into the named pipe
// FIXTURE_FIFO and then waits a minute, in the run that FIXTURE_PHASE
// names: opening, ordinary, mutant or closing.
const blocker = `package fixture

import (
	"os"
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	phase := "closing"
	if os.Getenv("DOKIMI_MUTATE_TRACE") != "" {
		phase = "opening"
	}
	if os.Getenv("DOKIMI_MUTATE_MUTANT") != "0" {
		phase = "mutant"
	}
	if os.Getenv("DOKIMI_MUTATE_INSTRUMENTED") == "" {
		phase = "ordinary"
	}
	if phase == os.Getenv("FIXTURE_PHASE") {
		f, err := os.OpenFile(os.Getenv("FIXTURE_FIFO"), os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte("x"))
		_ = f.Close()
		time.Sleep(time.Minute)
	}
	if Add(2, 3) != 5 {
		t.Error("Add(2, 3) != 5")
	}
}
`

// cancelIn runs the engine on the blocker fixture, with confirmation when
// confirm is true, and cancels the run when the fixture's test writes into
// the pipe in the run phase.
func cancelIn(t *testing.T, phase string, confirm bool) *record.Record {
	t.Helper()
	dir := module(
		t,
		map[string]string{
			"add.go":      "package fixture\n\nfunc Add(a, b int) int { return a + b }\n",
			"add_test.go": blocker,
		},
	)
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *record.Record, 1)
	go func() {
		rec, err := run.Run(
			ctx,
			run.Config{
				Dir:     dir,
				Env:     append(os.Environ(), "FIXTURE_FIFO="+fifo, "FIXTURE_PHASE="+phase),
				Confirm: confirm,
			},
		)
		if err != nil {
			t.Error(err)
		}
		done <- rec
		// A run that ended without writing leaves the reader blocked in
		// open, which this unblocks.
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	}()
	f, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := f.Read(make([]byte, 1))
	_ = f.Close()
	if n != 1 {
		t.Fatalf("the run ended before its %s control run", phase)
	}
	cancel()
	return <-done
}

func TestProcess(t *testing.T) {
	t.Parallel()
	add := "package fixture\n\nfunc Add(a, b int) int { return a + b }\n"
	t.Run("Run", func(t *testing.T) {
		t.Parallel()
		t.Run("gives error to a mutant whose run a signal that the engine did not send ends", func(t *testing.T) {
			t.Parallel()
			rec := runIn(t, module(t, map[string]string{"add.go": add, "add_test.go": killer}), run.Config{})
			want(t, verdicts(rec), "Add sbr-zero 0: error\nAdd aor 0: error\n")
			if reason := rec.Mutants[0].Reason; reason != "the run ended on the signal killed, which the engine did not send" ||
				rec.Score != nil {
				t.Errorf("reason %q, score %v", reason, rec.Score)
			}
		})
		t.Run("states control-failed when a signal ends the opening control run", func(t *testing.T) {
			t.Parallel()
			dir := module(t, map[string]string{"add.go": add, "add_test.go": killer})
			rec := runIn(t, dir, run.Config{Env: append(os.Environ(), "FIXTURE_ALWAYS=1")})
			want(t, codes(rec), "control-failed")
			if msg := rec.Errors[0].Message; !strings.HasPrefix(
				msg,
				"the tests fail with no mutant active: the run ended on the signal killed, which the engine did not send\n",
			) {
				t.Errorf("message %q", msg)
			}
		})
		t.Run("runs every go command with GOMAXPROCS set to Procs", func(t *testing.T) {
			t.Parallel()
			goCmd, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(t.TempDir(), "log")
			env := append(
				withoutKey(os.Environ(), "PATH"),
				"PATH="+goLog+string(filepath.ListSeparator)+os.Getenv("PATH"),
				"FIXTURE_GO="+goCmd,
				"FIXTURE_GO_LOG="+log,
				"FIXTURE_PROCS=3",
			)
			rec := runIn(
				t,
				module(t, map[string]string{"add.go": add, "add_test.go": procsTest}),
				run.Config{Env: env, Procs: 3},
			)
			want(t, verdicts(rec)+codes(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			// go env, go list, go test -c, and go tool buildid for the test
			// binary and for the engine's executable.
			want(t, string(data), "3\n3\n3\n3\n3\n")
		})
		t.Run("stops a confirmation whose build the caller cancels", func(t *testing.T) {
			t.Parallel()
			// The confirmation's build waits a minute after it creates the
			// marker, so the caller cancels the run during that build.
			env, marker := blocking(t, "*/confirm*/overlay.json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go cancelAt(ctx, cancel, marker)
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runWith(t, ctx, dir, run.Config{Env: env, Confirm: true})
			want(t, verdicts(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n"+
				"Sub sbr-zero 0: not-run\nSub aor 0: not-run\nUnused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			least := rec.Mutants[2].Key
			for _, m := range rec.Mutants[3:] {
				least = min(least, m.Key)
			}
			if rec.Mutants[2].Reason != "the caller cancelled the run" || rec.Sample == nil ||
				rec.Sample.Before != least {
				t.Errorf(
					"reason %q, sample %+v, want the sample before the least key %s",
					rec.Mutants[2].Reason,
					rec.Sample,
					least,
				)
			}
		})
		t.Run("stops a run whose ordinary build the caller cancels", func(t *testing.T) {
			t.Parallel()
			env, marker := blocking(t, "*/ordinary/pkg.test")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go cancelAt(ctx, cancel, marker)
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runWith(t, ctx, dir, run.Config{Env: env, Confirm: true})
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller cancelled the run" || rec.Control.Ordinary != nil ||
				len(rec.Errors) != 0 {
				t.Errorf("reason %q, ordinary %v, errors %v", rec.Mutants[0].Reason, rec.Control.Ordinary, rec.Errors)
			}
		})
		t.Run("stops a run whose instrumented build the caller cancels", func(t *testing.T) {
			t.Parallel()
			env, marker := blocking(t, "*/src/overlay.json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go cancelAt(ctx, cancel, marker)
			dir := module(t, map[string]string{"arith.go": arith, "arith_test.go": arithTest})
			rec := runWith(t, ctx, dir, run.Config{Env: env})
			want(t, verdicts(rec), notRun()+"Unused sbr-zero 0: not-run\nUnused aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller cancelled the run" || len(rec.Errors) != 0 {
				t.Errorf("reason %q, errors %v", rec.Mutants[0].Reason, rec.Errors)
			}
		})
		t.Run("stops at a cancellation during the ordinary control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelIn(t, "ordinary", true)
			want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller cancelled the run" || rec.Control.Ordinary != nil ||
				len(rec.Errors) != 0 {
				t.Errorf("reason %q, ordinary %v, errors %v", rec.Mutants[0].Reason, rec.Control.Ordinary, rec.Errors)
			}
		})
		t.Run("stops at a cancellation during the opening control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelIn(t, "opening", false)
			want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller cancelled the run" || rec.Control != nil || len(rec.Errors) != 0 {
				t.Errorf("reason %q, control %v, errors %v", rec.Mutants[0].Reason, rec.Control, rec.Errors)
			}
		})
		t.Run("stops at a cancellation during a mutant's run", func(t *testing.T) {
			t.Parallel()
			rec := cancelIn(t, "mutant", false)
			want(t, verdicts(rec), "Add sbr-zero 0: not-run\nAdd aor 0: not-run\n")
			if rec.Mutants[0].Reason != "the caller cancelled the run" || rec.Mutants[0].Seconds != nil ||
				rec.Control.Closing != nil {
				t.Errorf(
					"reason %q, seconds %v, closing %v",
					rec.Mutants[0].Reason,
					rec.Mutants[0].Seconds,
					rec.Control.Closing,
				)
			}
		})
		t.Run("stops at a cancellation during the closing control run", func(t *testing.T) {
			t.Parallel()
			rec := cancelIn(t, "closing", false)
			want(t, verdicts(rec), "Add sbr-zero 0: killed [TestAdd]\nAdd aor 0: killed [TestAdd]\n")
			if rec.Control.Closing != nil || len(rec.Errors) != 0 {
				t.Errorf("closing %v, errors %v", rec.Control.Closing, rec.Errors)
			}
		})
	})
}

// blocking returns the environment of a run whose go command creates the
// file marker and then waits a minute, in place of each command with an
// argument that matches the shell pattern block, and returns marker.
func blocking(t *testing.T, block string) (env []string, marker string) {
	t.Helper()
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	marker = filepath.Join(t.TempDir(), "marker")
	env = append(
		withoutKey(os.Environ(), "PATH"),
		"PATH="+goBlock+string(filepath.ListSeparator)+os.Getenv("PATH"),
		"FIXTURE_GO="+goCmd,
		"FIXTURE_MARKER="+marker,
		"FIXTURE_BLOCK="+block,
	)
	return env, marker
}

// cancelAt calls cancel once the file marker exists, unless ctx ends first.
func cancelAt(ctx context.Context, cancel context.CancelFunc, marker string) {
	for ctx.Err() == nil {
		if _, err := os.Stat(marker); err == nil {
			cancel()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
