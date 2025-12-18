package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
)

// InPlaceUI for low-flicker updates (robust mode: home + clear + redraw)
type InPlaceUI struct {
	out *bufio.Writer
}

func (ui *InPlaceUI) Init() error {
	// try to enable VT (Windows); non-windows is no-op
	if err := enableVT(); err != nil {
		// warn but continue; terminal may still support ANSI
		log.Printf("warning: enableVT failed: %v", err)
	}
	ui.out = bufio.NewWriterSize(os.Stdout, 1<<20)

	// Use alternate screen buffer to keep the UI isolated (optional but robust).
	// It also avoids scrollback pollution in many terminals.
	fmt.Fprint(ui.out, "\x1b[?1049h") // enter alternate screen
	fmt.Fprint(ui.out, "\x1b[2J")     // clear screen
	fmt.Fprint(ui.out, "\x1b[H")      // cursor home
	fmt.Fprint(ui.out, "\x1b[?25l")   // hide cursor

	return ui.out.Flush()
}

func (ui *InPlaceUI) Close() {
	if ui.out == nil {
		return
	}
	// restore cursor + leave alternate screen
	fmt.Fprint(ui.out, "\x1b[?25h")   // show cursor
	fmt.Fprint(ui.out, "\x1b[?1049l") // leave alternate screen
	_ = ui.out.Flush()
}

// Draw block (multi-line) in-place.
// This implementation does NOT rely on counting lines or terminal width.
// It always redraws from the top-left and clears the rest of the screen.
func (ui *InPlaceUI) Draw(block string) error {
	if ui.out == nil {
		return nil
	}

	// Go to top-left, clear from cursor to end of screen, then print the block.
	fmt.Fprint(ui.out, "\x1b[H")  // cursor home
	fmt.Fprint(ui.out, "\x1b[0J") // clear from cursor to end of screen
	fmt.Fprint(ui.out, block)     // draw new block

	return ui.out.Flush()
}
