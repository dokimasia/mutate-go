// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package record

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The domains of the two digests. Each is the first field of its digest's
// stream, so a files digest never equals an inputs digest of the same
// fields. The number after the slash is the version of the stream's layout.
const (
	filesDomain  = "dokimi-mutate-files/2"
	inputsDomain = "dokimi-mutate-inputs/2"
)

// inputsPrefix starts an inputs digest and names its hash function.
const inputsPrefix = "sha256:"

// lengthEnd ends the decimal length before each field of a digest's stream.
const lengthEnd = ":"

// dirtySuffix ends the version that the go command stamps on a build from a
// modified working tree.
const dirtySuffix = "+dirty"

// identitySeparator separates an engine's version from its build ID in the
// engine's identity.
const identitySeparator = " "

// Files returns the digest of a package's data files, as hexadecimal.
// snapshot maps the path of each file to the file's digest. The digest's
// stream contains each path and its file's digest in the order of the
// paths, each behind its length, so no two snapshots share a stream.
func Files(snapshot map[string]string) string {
	h := sha256.New()
	field(h, filesDomain)
	for _, path := range slices.Sorted(maps.Keys(snapshot)) {
		field(h, path)
		field(h, snapshot[path])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Inputs returns the inputs digest of a run, as sha256:<hex>. Two runs have
// one digest exactly when their engine's identity, their toolchain, the
// build IDs of their instrumented test binaries, and the digests of the
// packages' data that Files returns are equal. buildIDs and files each state
// every test binary or package of the run's suite, in the run's order.
func Inputs(engine, toolchain, buildIDs, files string) string {
	h := sha256.New()
	for _, f := range []string{inputsDomain, engine, toolchain, buildIDs, files} {
		field(h, f)
	}
	return inputsPrefix + hex.EncodeToString(h.Sum(nil))
}

// Identity returns what the inputs digest states of the engine: version,
// which identifies a released engine's code, and after it buildID, the
// build ID of the engine's executable, when version is (devel) or ends in
// +dirty. The go command states such a version for a build of code that no
// version identifies.
func Identity(version, buildID string) string {
	if version == develVersion || strings.HasSuffix(version, dirtySuffix) {
		return version + identitySeparator + buildID
	}
	return version
}

// field writes s to h behind its decimal length and lengthEnd.
func field(h hash.Hash, s string) {
	h.Write([]byte(strconv.Itoa(len(s)) + lengthEnd + s))
}
