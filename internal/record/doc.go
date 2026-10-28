// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

// Package record states a mutation run's record, the JSON document that
// the run protocol defines for one run of one target, and computes its
// score, its inputs digest and its requirements digest. [Read] reads the
// record of a history, and [Record.Write] replaces a record file whole.
//
// # Errors
//
// Read returns [ErrForeign] for a record of another format, version or
// engine.
//
// # Dependency position
//
// Imports crypto/sha256, encoding/hex, encoding/json, errors, fmt, hash,
// maps, net/url, os, path/filepath, runtime/debug, slices, strconv and
// strings from the standard library, and internal/spec from this module.
package record
