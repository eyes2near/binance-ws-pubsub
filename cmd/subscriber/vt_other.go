//go:build !windows
// +build !windows

package main

// enableVT is a no-op on non-Windows platforms (terminals usually support ANSI already).
func enableVT() error { return nil }
