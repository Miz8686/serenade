package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// selftest exercises the full pipeline without a terminal:
// backend spawn, library index, status poll. Exit 0 = all green.
func selftest() int {
	fail := func(format string, a ...any) int {
		fmt.Printf("FAIL: "+format+"\n", a...)
		return 1
	}
	t0 := time.Now()
	be, err := ensureBackend()
	if err != nil {
		return fail("backend: %v", err)
	}
	fmt.Printf("ok: backend up in %v\n", time.Since(t0).Round(time.Second))
	tracks, err := indexLibrary()
	if err != nil {
		return fail("index: %v", err)
	}
	fmt.Printf("ok: indexed %d tracks\n", len(tracks))
	st, err := be.status()
	if err != nil {
		return fail("status: %v", err)
	}
	fmt.Printf("ok: status=%q file=%q\n", st.State, st.File)
	if msg := vizSmoke(); msg != "" {
		return fail("viz: %s", msg)
	}
	fmt.Println("ok: viz tap+fft smoke")
	m := newModel(be, false, defaultConfig())
	m.tracks = tracks
	m.indexing = false
	m.width, m.height = 167, 39
	m.status = st
	out := m.View()
	if len(out) < 100 {
		return fail("view too short: %d", len(out))
	}
	fmt.Printf("ok: view renders (%d bytes)\n", len(out))
	if msg := mouseCheck(); msg != "" {
		return fail("mouse: %s", msg)
	}
	fmt.Println("ok: mouse geometry (click/double-click/wheel gating)")
	fmt.Println("SELFTEST PASS")
	return 0
}

// vizSmoke starts the tap, waits for real frames, times FFT cost.
// Proves the tap+FFT path against live PipeWire; fails honestly when
// nothing is playing (no signal to measure).
func vizSmoke() string {
	t := &vizTap{}
	t.start()
	defer t.stop()
	if _, broken := t.state(); broken != "" {
		return "tap: " + broken
	}
	deadline := time.Now().Add(4 * time.Second)
	var frame []float64
	for time.Now().Before(deadline) {
		if frame = t.frame(vizFFTSize); frame != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if frame == nil {
		return "no frames in 4s (paused? tap buffering?)"
	}
	t0 := time.Now()
	for i := 0; i < 100; i++ {
		c := append([]float64(nil), frame...)
		fftLevels(c, vizRate, 24)
	}
	per := time.Since(t0) / 100
	levels := fftLevels(append([]float64(nil), frame...), vizRate, 24)
	peak, sum := 0.0, 0.0
	for _, l := range levels {
		sum += l
		if l > peak {
			peak = l
		}
	}
	fmt.Printf("ok: fft 100x avg %v/frame, live peak %.2f mean %.3f\n", per, peak, sum/float64(len(levels)))
	return ""
}

// mouseCheck drives handleMouse with synthetic events against a known
// layout (167x39, 10 fake tracks) and asserts row mapping, focus
// switching, double-click, and wheel gating. This is the regression
// net for the geometry handleMouse depends on.
func mouseCheck() string {
	mk := func() model {
		var tr []Track
		for i := 0; i < 10; i++ {
			tr = append(tr, Track{Path: fmt.Sprintf("/t%d.flac", i), Artist: "A", Title: fmt.Sprintf("T%d", i)})
		}
		m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
		m.tracks = tr
		m.view = tr
		m.indexing = false
		m.width, m.height = 167, 39
		m.focus = focusDetail // prove clicks steal focus back
		m.clampCursor()
		return m
	}
	click := func(m model, x, y int) model {
		um, _ := m.handleMouse(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		return um.(model)
	}
	// Rows start at Y=2 (border + title), X=1..98 is list interior.
	m := mk()
	m = click(m, 5, 5) // row 3
	if m.cursor != 3 {
		return fmt.Sprintf("click row: cursor=%d, want 3", m.cursor)
	}
	if m.focus != focusList {
		return "click did not steal focus to list"
	}
	// Double-click same row plays (backend dead -> beErr, must not panic).
	m = click(m, 5, 5)
	if m.cursor != 3 {
		return fmt.Sprintf("double-click moved cursor to %d", m.cursor)
	}
	// Wheel inside list scrolls; wheel over detail must not touch cursor.
	m = mk()
	m.cursor = 5
	m.clampCursor()
	um, _ := m.handleMouse(tea.MouseMsg{X: 5, Y: 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if um.(model).cursor != 2 {
		return "wheel-up in list did not scroll"
	}
	m2 := mk()
	m2.cursor = 5
	m2.clampCursor()
	um2, _ := m2.handleMouse(tea.MouseMsg{X: 130, Y: 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if um2.(model).cursor != 5 {
		return "wheel over detail moved list cursor"
	}
	// Click in status bar area: no-op.
	m3 := mk()
	um3, _ := m3.handleMouse(tea.MouseMsg{X: 5, Y: 38, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if um3.(model).cursor != 0 {
		return "click outside list moved cursor"
	}
	// Transport buttons: prev [1,4), play [5,8), next [9,12) at Y=37.
	m4 := mk()
	um4, _ := m4.handleMouse(tea.MouseMsg{X: 6, Y: 37, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if um4.(model).flash != "play" {
		return "play button click did not flash"
	}
	m5 := mk()
	um5, _ := m5.handleMouse(tea.MouseMsg{X: 60, Y: 37, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if um5.(model).flash != "" {
		return "click outside buttons flashed"
	}
	return ""
}
