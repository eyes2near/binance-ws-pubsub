package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// InPlaceUI for low-flicker updates
type InPlaceUI struct {
	out       *bufio.Writer
	lastLines int
}

func (ui *InPlaceUI) Init() error {
	// try to enable VT (Windows); non-windows is no-op
	if err := enableVT(); err != nil {
		// warn but continue; terminal may still support ANSI
		log.Printf("warning: enableVT failed: %v", err)
	}
	ui.out = bufio.NewWriterSize(os.Stdout, 1<<20)
	// hide cursor
	fmt.Fprint(ui.out, "\x1b[?25l")
	return ui.out.Flush()
}

func (ui *InPlaceUI) Close() {
	if ui.out == nil {
		return
	}
	// restore cursor
	fmt.Fprint(ui.out, "\x1b[?25h")
	_ = ui.out.Flush()
}

// Draw block (multi-line) in-place with minimal flicker
func (ui *InPlaceUI) Draw(block string) error {
	block = strings.TrimRight(block, "\n")
	lines := []string{""}
	if block != "" {
		lines = strings.Split(block, "\n")
	}

	// Try to detect terminal width to correctly account for wrapped lines.
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}

	// Calculate physical terminal lines the current block will occupy.
	// Use integer round-up to account for wrapped lines correctly.
	currPhysical := 0
	for _, ln := range lines {
		sw := runewidth.StringWidth(ln)
		if width <= 0 {
			width = 80
		}
		need := (sw + width - 1) / width
		if need <= 0 {
			need = 1
		}
		currPhysical += need
	}

	// Move cursor up by the previous physical line count so we land
	// at the true top of the previous block even if lines wrapped.
	if ui.lastLines > 0 {
		fmt.Fprintf(ui.out, "\x1b[%dA", ui.lastLines)
	}

	// Print the new block, clearing to end-of-line for each logical line.
	for i, ln := range lines {
		fmt.Fprint(ui.out, "\r")
		fmt.Fprint(ui.out, ln)
		fmt.Fprint(ui.out, "\x1b[0K")
		if i != len(lines)-1 {
			fmt.Fprint(ui.out, "\n")
		}
	}

	// If the previous block occupied more physical lines than the current one,
	// clear the remaining lines to remove any leftover wrapped content.
	if ui.lastLines > currPhysical {
		extra := ui.lastLines - currPhysical
		for i := 0; i < extra; i++ {
			// Move to start of line, clear it and move down
			fmt.Fprint(ui.out, "\r\x1b[0K")
			if i != extra-1 {
				fmt.Fprint(ui.out, "\n")
			}
		}
	}

	// Remember the number of physical lines we just rendered.
	ui.lastLines = currPhysical
	return ui.out.Flush()
}
