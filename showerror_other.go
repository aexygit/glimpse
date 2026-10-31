//go:build !windows

package main

// showError is a no-op off Windows, where the binary keeps its console and
// the slog line reaches stderr.
func showError(_, _ string) {}
