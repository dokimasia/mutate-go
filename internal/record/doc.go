// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

// Package record states a mutation run's record, the JSON document that
// the run protocol defines for one run of one target, and computes its
// score and its inputs digest.
//
// # Dependency position
//
// Imports crypto/sha256, encoding/hex, encoding/json, fmt, hash, maps,
// net/url, os, path/filepath, runtime/debug, slices, strconv and strings
// from the standard library, and internal/spec from this module.
package record
