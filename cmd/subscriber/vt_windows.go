//go:build windows
// +build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT enables ENABLE_VIRTUAL_TERMINAL_PROCESSING on the Windows console.
func enableVT() error {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return err
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	return windows.SetConsoleMode(h, mode)
}
