// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"go.dokimi.dev/mutate/internal/load"
	"go.dokimi.dev/mutate/internal/spec"
	"go.dokimi.dev/mutate/internal/testbin"
)

// The arguments of the go commands that build a test binary and read a
// build ID. A build runs without vet, because the vet of Go 1.21 to 1.23
// opens a file that only the overlay adds on disk, where it does not exist.
var (
	compileArgs = []string{"test", "-c", "-vet=off"}
	buildIDArgs = []string{"tool", "buildid"}
)

// The flags of a build that name its output and its overlay.
const (
	outputFlag  = "-o"
	overlayFlag = "-overlay"
)

// The names in the run's work directory: the instrumented source, the
// instrumented test binary of the package's own tests, the start of the
// name of another package's test binary, the suffix of a test binary, and
// the directories of the builds of the unchanged source.
const (
	sourceDir     = "src"
	ownBinary     = "pkg"
	suiteBinary   = "suite"
	binarySuffix  = ".test"
	unchangedDir  = "unchanged"
	ordinaryDir   = "ordinary"
	confirmPrefix = "confirm"
)

// packageLine starts a line of the go command's error that names the
// package whose build failed.
const packageLine = "#"

// build builds the instrumented test binary of each program and reads its
// build ID. It returns the reason that the run stops before the opening
// control run, or "" when every program built. err is the error of the
// instrumentation, which fails the build too.
func (r *runner) build(ctx context.Context, err error) string {
	var overlay string
	if err == nil {
		overlay, err = r.prog.Write(filepath.Join(r.work, sourceDir))
	}
	for i := 0; err == nil && i < len(r.programs); i++ {
		p := r.programs[i]
		name := ownBinary
		if p.target != "" {
			name = suiteBinary + strconv.Itoa(i)
		}
		p.bin = filepath.Join(r.work, name+binarySuffix+testbin.ExeSuffix)
		if err = r.compile(ctx, r.goEnv, p, p.bin, overlay); err != nil {
			return r.buildFailed(ctx, p, err)
		}
		p.buildID, err = buildID(ctx, r.pkg.Dir, r.goEnv, p.bin)
	}
	if err != nil {
		r.fail(spec.ErrorBuild, err.Error())
		return "the instrumented build failed"
	}
	return ""
}

// buildFailed states the failure err of the instrumented build of p, and
// returns the reason that the run stops. It builds p from the unchanged
// source too, so the run error states whether the tests build without the
// engine. A build that ctx ended states no run error.
func (r *runner) buildFailed(ctx context.Context, p *program, err error) string {
	_, stop := r.ordinaryBinary(ctx, filepath.Join(r.work, unchangedDir), p, "")
	switch {
	case ctx.Err() != nil:
		return cancelled
	case stop == nil:
		r.fail(spec.ErrorBuild, "the instrumented build fails, and the unchanged source builds: "+err.Error())
		return "the instrumented build failed"
	}
	r.fail(spec.ErrorBuild, "the tests"+of(p.target)+" do not build from the unchanged source: "+stop.reason)
	return "the tests do not build"
}

// ordinaryBinary builds the test binary of p into dir, from the package's
// source with the files of overlay in place of its own, and returns its
// path. Without an overlay, it builds the unchanged source. A build that
// does not give a binary returns its outcome instead: not-run when ctx
// ended it, and not-viable with the toolchain's message otherwise.
func (r *runner) ordinaryBinary(ctx context.Context, dir string, p *program, overlay string) (string, *outcome) {
	bin := filepath.Join(dir, filepath.Base(p.bin))
	if err := r.compile(ctx, r.confirmEnv, p, bin, overlay); err != nil {
		if ctx.Err() != nil {
			return "", &outcome{verdict: spec.NotRun, reason: cancelled}
		}
		return "", &outcome{verdict: spec.NotViable, reason: compilerMessage(err)}
	}
	return bin, nil
}

// compile builds the test binary of p into bin with go test -c in the
// package directory, under the environment env, from the package's source
// with the files of overlay in place of its own. An empty overlay builds the
// unchanged source.
func (r *runner) compile(ctx context.Context, env []string, p *program, bin, overlay string) error {
	args := append(append([]string{}, compileArgs...), outputFlag, bin)
	if overlay != "" {
		args = append(args, overlayFlag, overlay)
	}
	_, err := load.Go(ctx, r.pkg.Dir, env, append(args, p.pattern())...)
	return err
}

// buildID returns the build ID of the executable at path, as go tool
// buildid reports it in dir under the environment env, or the error of the
// go command.
func buildID(ctx context.Context, dir string, env []string, path string) (string, error) {
	id, err := load.Go(ctx, dir, env, append(append([]string{}, buildIDArgs...), path)...)
	return strings.TrimSpace(string(id)), err
}

// compilerMessage returns the toolchain's message of a failed build: the
// lines of the error after the go command's own, without the lines that
// name a package.
func compilerMessage(err error) string {
	var lines []string
	for _, line := range strings.Split(err.Error(), "\n")[1:] {
		if !strings.HasPrefix(line, packageLine) {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
