// Copyright Dokimasia B.V. 2026
// SPDX-License-Identifier: MIT

package record

import "errors"

// ErrForeign reports a record that is no history of the engine: a record of
// another format, of another layout version, or of another engine. [Read]
// returns it.
var ErrForeign = errors.New("record: the record is of another format, version or engine")
