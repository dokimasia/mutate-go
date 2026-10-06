// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

//go:build !linux

package main

// memoryLimit returns 0, and the command then admits every package at once.
// The engine applies a memory ceiling on Linux alone, so a budget would
// count nothing elsewhere.
func memoryLimit(_, _ string) int64 { return 0 }
