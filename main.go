// Command serenade is a minimal Phase-1 terminal music player.
//
// Architecture: the Go binary owns ALL UI state and rendering. Audio
// playback is delegated to a headless cmus backend over cmus's own
// socket protocol (via the cmus-remote CLI). No decoders, no threads
// doing DSP, no shell-script glue.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--selftest" {
		os.Exit(selftest())
	}
	// Kitty probe runs concurrently with backend spawn: both are
	// independent, so startup costs max(), not sum().
	kittyCh := make(chan bool, 1)
	go func() { kittyCh <- kittyProbe() }()
	be, err := ensureBackend()
	if err != nil {
		fmt.Fprintf(os.Stderr, "serenade: backend failed: %v (see ~/.cache/serenade/cmus.log)\n", err)
		fmt.Fprintln(os.Stderr, "serenade: starting in library-only mode")
	}
	m := newModel(be, <-kittyCh, loadConfig())
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithReportFocus(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "serenade: %v\n", err)
		os.Exit(1)
	}
}
