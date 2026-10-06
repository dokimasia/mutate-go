// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package process_test

// platformFakes are the behaviours of the fake command that need a
// platform. Windows has none: a group there is the command's process alone.
var platformFakes = map[string]func() int{}
