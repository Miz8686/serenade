package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

func TestIndexLibrary(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if len(tracks) < 200 {
		t.Fatalf("expected 200+ tracks, got %d", len(tracks))
	}
	t.Logf("indexed %d tracks, first: %s", len(tracks), tracks[0].label())
}

func TestBackendSupervisor(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	be, err := ensureBackend()
	if err != nil {
		t.Fatalf("backend: %v", err)
	}
	st, err := be.status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("backend alive, state=%q", st.State)
}

func TestViewRenders(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	be := &backend{sock: "/nonexistent.sock"}
	m := newModel(be, false, defaultConfig())
	m.tracks = []Track{{Path: "/x.flac", Artist: "A", Title: "T", Album: "Al"}}
	m.indexing = false
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 167, Height: 39})
	mm := updated.(model)
	out := mm.View()
	if len(out) < 100 {
		t.Fatalf("view too short: %d", len(out))
	}
	t.Logf("view bytes: %d", len(out))
}

func TestNextTrackPath(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}}
	if p, ok := nextTrackPath(tr, "/b"); !ok || p != "/c" {
		t.Fatalf("mid advance: %q %v", p, ok)
	}
	if p, ok := nextTrackPath(tr, "/c"); !ok || p != "/a" {
		t.Fatalf("wrap advance: %q %v", p, ok)
	}
	if _, ok := nextTrackPath(tr, "/missing"); ok {
		t.Fatalf("unknown path should not advance")
	}
	if _, ok := nextTrackPath(nil, "/a"); ok {
		t.Fatalf("empty library should not advance")
	}
}

func TestFuzzyFilter(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.tracks = []Track{
		{Path: "/a", Artist: "Linkin Park", Title: "Papercut"},
		{Path: "/b", Artist: "Linkin Park", Title: "Numb"},
		{Path: "/c", Artist: "ABBA", Title: "Mamma Mia"},
	}
	m.view = m.tracks
	m.searchBox.SetValue("papercut")
	m.applyFilter()
	if len(m.view) != 1 || m.view[0].Path != "/a" {
		t.Fatalf("filter wrong: %+v", m.view)
	}
	if len(m.viewHL) != 1 || len(m.viewHL[0]) == 0 {
		t.Fatalf("missing highlight indexes")
	}
	m.searchBox.SetValue("")
	m.applyFilter()
	if len(m.view) != 3 {
		t.Fatalf("clear did not restore: %d", len(m.view))
	}
}

func TestPickNext(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}}
	// Queue drains first, preserving order.
	n, rest, ok := pickNext([]string{"/q1", "/q2"}, tr, "/a", nil)
	if !ok || n != "/q1" || len(rest) != 1 || rest[0] != "/q2" {
		t.Fatalf("queue drain: %q %v %v", n, rest, ok)
	}
	// Empty queue falls back to library order.
	n, rest, ok = pickNext(nil, tr, "/a", nil)
	if !ok || n != "/b" || len(rest) != 0 {
		t.Fatalf("fallback: %q %v %v", n, rest, ok)
	}
	// Empty everything: no advance.
	if _, _, ok = pickNext(nil, nil, "/a", nil); ok {
		t.Fatalf("empty should not advance")
	}
}

func TestHelpOverlay(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.showHelp = true
	out := m.View()
	for _, want := range []string{"Fuzzy search", "Queue add", "Play selected"} {
		if !strings.Contains(out, want) {
			t.Fatalf("overlay missing %q", want)
		}
	}
}

// Locks the selection/playing-row distinction: the playing row must
// render distinctly from both a plain row and the cursor row, so the
// two can never silently merge during refactors.
func TestPlayingRowDistinct(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	buildBaseStyles()
	plain := "  artist - title"
	cursor := styleSelected.Render(plain)
	playing := stylePlaying.Render(iconPlaying + " artist - title")
	if cursor == playing {
		t.Fatalf("cursor and playing-row styles are identical")
	}
	if playing == plain || cursor == plain {
		t.Fatalf("styled rows must differ from plain text")
	}
}

func TestShuffleBag(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}, {Path: "/d"}}
	sh := &shuffleState{on: true, order: freshBag(len(tr), -1)}
	seen := map[string]bool{}
	for i := 0; i < len(tr); i++ {
		n, rest, ok := pickNext(nil, tr, "", sh)
		if !ok {
			t.Fatalf("bag walk stopped at %d", i)
		}
		if seen[n] {
			t.Fatalf("repeat within one pass: %q", n)
		}
		seen[n] = true
		_ = rest
	}
	if len(seen) != len(tr) {
		t.Fatalf("bag covered %d/%d", len(seen), len(tr))
	}
	// Exhaustion reshuffles instead of stopping.
	n, _, ok := pickNext(nil, tr, "", sh)
	if !ok || n == "" {
		t.Fatalf("reshuffle failed")
	}
}

func TestShuffleReshuffleBoundary(t *testing.T) {
	// 50 trials: after exhaustion, first track of the new pass must
	// not equal the last track of the old pass (when avoidable).
	for trial := 0; trial < 50; trial++ {
		tr := []Track{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}}
		sh := &shuffleState{on: true, order: []int{0, 1, 2}, pos: 3}
		n, _, ok := pickNext(nil, tr, "/c", sh)
		if !ok {
			t.Fatalf("reshuffle failed")
		}
		if n == "/c" {
			t.Fatalf("boundary repeat: new pass starts with just-played track")
		}
	}
}

func TestQueueBeatsShuffle(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}}
	sh := &shuffleState{on: true, order: []int{1, 0}, pos: 0}
	n, rest, ok := pickNext([]string{"/q"}, tr, "/a", sh)
	if !ok || n != "/q" || len(rest) != 0 {
		t.Fatalf("queue must win: %q %v", n, rest)
	}
	if sh.pos != 0 {
		t.Fatalf("queue drain must not consume bag position")
	}
}

func TestIsNaturalEnd(t *testing.T) {
	P := func(state, file string) Status { return Status{State: state, File: file} }
	cases := []struct {
		prev, cur Status
		want      bool
	}{
		{P("playing", "/a"), P("stopped", "/a"), true},  // held file
		{P("playing", "/a"), P("stopped", ""), true},    // cleared file
		{P("playing", "/a"), P("stopped", "/b"), false}, // external action
		{P("playing", "/a"), P("paused", "/a"), false},
		{P("stopped", "/a"), P("stopped", "/a"), false},
		{P("playing", ""), P("stopped", "/a"), false},
		{P("playing", "/a"), P("playing", "/b"), false},
	}
	for _, c := range cases {
		if got := isNaturalEnd(c.prev, c.cur); got != c.want {
			t.Fatalf("isNaturalEnd(%v, %v) = %v, want %v", c.prev, c.cur, got, c.want)
		}
	}
}

func TestPrevTrackPath(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}, {Path: "/c"}}
	if p, ok := prevTrackPath(tr, "/b"); !ok || p != "/a" {
		t.Fatalf("mid prev: %q %v", p, ok)
	}
	if p, ok := prevTrackPath(tr, "/a"); !ok || p != "/c" {
		t.Fatalf("wrap prev: %q %v", p, ok)
	}
	if p, ok := prevTrackPath(tr, "/missing"); !ok || p != "/c" {
		t.Fatalf("unknown falls back to last: %q %v", p, ok)
	}
	if _, ok := prevTrackPath(nil, "/a"); ok {
		t.Fatalf("empty must not advance")
	}
}

func TestBarZone(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	y, x0, x1 := m.barZone()
	if y != 36 || x0 != 1 || x1 != 138 {
		t.Fatalf("barZone = %d,%d,%d want 36,1,138", y, x0, x1)
	}
}

func TestClickPrevDispatches(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{{Path: "/a"}, {Path: "/b"}}
	m.view = m.tracks
	m.status = Status{State: "playing", File: "/b"}
	// prev button zone starts at content X=0 -> screen X=1, Y=37.
	um, _ := m.handleMouse(tea.MouseMsg{X: 1, Y: 37, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	mm := um.(model)
	if mm.flash != "prev" {
		t.Fatalf("prev click did not dispatch (flash=%q)", mm.flash)
	}
	if mm.beErr == "" {
		t.Fatalf("playPrev did not attempt backend call (dead backend must error)")
	}
}

func TestFollowPlaying(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39 // visibleRows = 33
	var tracks []Track
	for i := 0; i < 50; i++ {
		tracks = append(tracks, Track{Path: "/t" + string(rune('a'+i))})
	}
	m.tracks, m.view = tracks, tracks
	m.cursor, m.offset = 0, 0
	m.followPlaying(tracks[40].Path)
	if m.cursor != 40 {
		t.Fatalf("cursor = %d, want 40", m.cursor)
	}
	// Centered: 40-16=24, clamped to len-vis=17.
	if m.offset != 17 {
		t.Fatalf("offset = %d, want 17", m.offset)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+m.visibleRows() {
		t.Fatalf("cursor %d not visible in [%d,%d)", m.cursor, m.offset, m.offset+m.visibleRows())
	}
	// Unknown path and empty path leave cursor alone.
	m.followPlaying("/missing")
	if m.cursor != 40 {
		t.Fatalf("unknown path moved cursor to %d", m.cursor)
	}
	m.followPlaying("")
	if m.cursor != 40 {
		t.Fatalf("empty path moved cursor to %d", m.cursor)
	}
}

func TestIdleVizUsesAccent(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.vizActive = false
	m.artPrim, m.artSec = "#D9A44C", "#6FA598"
	out := m.renderViz()
	// #D9A44C = 217,164,76 — the idle strip must carry it raw.
	if !strings.Contains(out, "\x1b[38;2;217;164;76m") {
		t.Fatalf("idle viz missing accent color: %q", out[:min(120, len(out))])
	}
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("idle viz must be a single compact line, got %d newlines", strings.Count(out, "\n"))
	}
}

func TestColdTransitionFollows(t *testing.T) {
	// Nothing playing, cursor cold at 0: the very first track-start
	// must leave cursor AND playing marker on the same row, with no
	// input beyond the status update itself.
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	var tracks []Track
	for i := 0; i < 50; i++ {
		tracks = append(tracks, Track{Path: "/t" + string(rune('a'+i))})
	}
	m.tracks, m.view = tracks, tracks
	m.cursor, m.offset = 0, 0
	um, _ := m.Update(statusMsg{st: Status{State: "playing", File: tracks[40].Path}})
	mm := um.(model)
	if mm.cursor != 40 {
		t.Fatalf("cold transition: cursor = %d, want 40", mm.cursor)
	}
	if mm.cursor < mm.offset || mm.cursor >= mm.offset+mm.visibleRows() {
		t.Fatalf("cold transition: cursor %d not visible", mm.cursor)
	}
	if mm.playingPath() != tracks[40].Path {
		t.Fatalf("playing marker = %q", mm.playingPath())
	}
}

func TestNaturalEndAdvances(t *testing.T) {
	// Locks the auto-advance-on-natural-end behavior end to end:
	// playing→stopped on the held file must attempt the next track.
	// A future mid-session stop feature that reuses this path will
	// trip here (see isNaturalEnd's ambiguity note) — that trip is
	// the point: disambiguate there, update here.
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{{Path: "/a"}, {Path: "/b"}}
	m.view = m.tracks
	m.status = Status{State: "playing", File: "/a"}
	m.prev = m.status
	um, _ := m.Update(statusMsg{st: Status{State: "stopped", File: "/a"}})
	mm := um.(model)
	if mm.beErr == "" {
		t.Fatalf("natural end did not attempt the next track (no playFile call)")
	}
}

func TestQueueMove(t *testing.T) {
	q := []string{"a", "b", "c"}
	q, c := queueMove(q, 1, -1)
	if c != 0 || q[0] != "b" || q[1] != "a" || q[2] != "c" {
		t.Fatalf("move up: %v cursor %d", q, c)
	}
	q, c = queueMove(q, 0, -1) // boundary: no-op
	if c != 0 || q[0] != "b" {
		t.Fatalf("top boundary must hold: %v cursor %d", q, c)
	}
	q, c = queueMove(q, 2, +1) // boundary: no-op
	if c != 2 || q[2] != "c" {
		t.Fatalf("bottom boundary must hold: %v cursor %d", q, c)
	}
	q, c = queueMove(q, 0, +1)
	if c != 1 || q[0] != "a" || q[1] != "b" {
		t.Fatalf("move down: %v cursor %d", q, c)
	}
	if _, c := queueMove(nil, 0, -1); c != 0 {
		t.Fatalf("empty move cursor %d", c)
	}
}

func TestQueueRemove(t *testing.T) {
	q, c := queueRemove([]string{"a", "b", "c"}, 1)
	if len(q) != 2 || q[0] != "a" || q[1] != "c" || c != 1 {
		t.Fatalf("remove middle: %v cursor %d", q, c)
	}
	q, c = queueRemove(q, 1) // remove last: cursor steps back
	if len(q) != 1 || q[0] != "a" || c != 0 {
		t.Fatalf("remove last: %v cursor %d", q, c)
	}
	q, c = queueRemove(q, 0)
	if len(q) != 0 || c != 0 {
		t.Fatalf("remove only: %v cursor %d", q, c)
	}
	if _, c := queueRemove(nil, 0); c != 0 {
		t.Fatalf("empty remove cursor %d", c)
	}
}

func TestQueueViewRenders(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{
		{Path: "/a", Artist: "Art", Title: "One"},
		{Path: "/b", Artist: "Art", Title: "Two"},
	}
	m.queue = []string{"/b", "/a"}
	m.showQueue = true
	out := m.queueView()
	for _, want := range []string{"queue (2)", "Art – Two", "Art – One", "d remove"} {
		if !strings.Contains(out, want) {
			t.Fatalf("queue view missing %q", want)
		}
	}
	if strings.Index(out, "Art – Two") > strings.Index(out, "Art – One") {
		t.Fatalf("queue view must list in play order")
	}
	m.queue = nil
	if out := m.queueView(); !strings.Contains(out, "empty") {
		t.Fatalf("empty queue needs an empty state, got %q", out[:min(80, len(out))])
	}
}

func TestListWindowGrouping(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{
		{Path: "/a1", Artist: "Alpha", Title: "one"},
		{Path: "/a2", Artist: "Alpha", Title: "two"},
		{Path: "/b1", Artist: "Beta", Title: "one"},
	}
	m.view = m.tracks
	m.cursor, m.offset = 0, 0
	lines := m.listWindow()
	want := []int{llArtist, llTrack, llTrack, llGap, llArtist, llTrack}
	if len(lines) != len(want) {
		t.Fatalf("window has %d lines, want %d", len(lines), len(want))
	}
	for i, k := range want {
		if lines[i].kind != k {
			t.Fatalf("line %d kind=%d want %d", i, lines[i].kind, k)
		}
	}
	if got := m.screenSpan(0, 2); got != len(lines) {
		t.Fatalf("screenSpan(0,2)=%d != window len %d", got, len(lines))
	}
	// Artist-less runs get no heading: titles carry their own context.
	m.view = append([]Track{{Path: "/x", Title: "No Artist - Track"}}, m.view...)
	m.cursor, m.offset = 0, 0
	lines = m.listWindow()
	if lines[0].kind != llTrack {
		t.Fatalf("artist-less lead must render bare, got kind=%d", lines[0].kind)
	}
	if got := m.screenSpan(0, 3); got != len(lines) {
		t.Fatalf("screenSpan(0,3)=%d != window len %d", got, len(lines))
	}
	// Heading clicks are ignored; track clicks resolve view indexes.
	um, _ := m.handleMouse(tea.MouseMsg{X: 5, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if um.(model).cursor != 0 {
		t.Fatalf("heading click moved cursor")
	}
	// window: Tx(0) G(1) H(2) Ta1(3) Ta2(4) G(5) H(6) Tb1(7) -> Y=9
	um, _ = m.handleMouse(tea.MouseMsg{X: 5, Y: 9, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if um.(model).cursor != 3 {
		t.Fatalf("post-gap click: cursor=%d want 3", um.(model).cursor)
	}
}

func TestMeterBar(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39 // bar width = 137
	m.artPrim, m.artSec = "#D9A44C", "#6FA598"
	out := m.renderBar(0.5)
	// strip escapes, count cells
	cells := 0
	filled := 0
	for _, r := range out {
		if r == '█' {
			cells++
			filled++
		} else if r == '─' {
			cells++
		}
	}
	if cells != 137 {
		t.Fatalf("bar renders %d cells, zone assumes 137", cells)
	}
	if filled != 68 { // int(0.5*137)
		t.Fatalf("filled=%d want 68", filled)
	}
	// gradient endpoints: first cell sits at the prim end of the ramp
	cr, cg, cb := hexLerp("#D9A44C", "#6FA598", 0.5/137)
	head := fmt.Sprintf("\x1b[38;2;%d;%d;%dm\u2588", cr, cg, cb)
	if !strings.Contains(out, head) {
		t.Fatalf("bar head missing gradient start %q", head)
	}
	if strings.Contains(out, "\x1b[38;2;217;164;76m─") {
		t.Fatalf("empty cells must not use the gradient")
	}
	if got := m.renderBar(0); strings.Contains(got, "█") {
		t.Fatalf("zero progress must render no filled cells")
	}
}

func TestIconWidths(t *testing.T) {
	// Every transport/marker glyph must stay single-cell: button
	// zones and list alignment are derived from runewidth.
	for _, g := range []string{iconPlaying, iconPrev, iconPlay, iconPause, iconNext, iconShuffle} {
		if w := runewidth.StringWidth(g); w != 1 {
			t.Fatalf("icon U+%04X width=%d, want 1", []rune(g)[0], w)
		}
	}
}

func TestRevealStops(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.artImg = stripeImage(144, 144, 6, 0.8, 0.8)
	m.reveal = 2
	um, cmd := m.Update(revealTickMsg{})
	mm := um.(model)
	if mm.reveal != 1 || cmd == nil {
		t.Fatalf("mid-wipe must continue: reveal=%d cmd-nil=%v", mm.reveal, cmd == nil)
	}
	um, cmd = mm.Update(revealTickMsg{})
	mm = um.(model)
	if mm.reveal != 0 || cmd != nil {
		t.Fatalf("wipe must stop after fixed frames: reveal=%d cmd-nil=%v", mm.reveal, cmd == nil)
	}
	if mm.artBlock != mm.renderArt() {
		t.Fatalf("settled wipe must equal the plain render")
	}
}

func TestRevealStartsOnTransition(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var a, b Track
	for _, tr := range tracks {
		if strings.HasSuffix(tr.Path, "Papercut.flac") {
			a = tr
		}
		if strings.HasSuffix(tr.Path, "Faint.flac") {
			b = tr
		}
	}
	if a.Path == "" || b.Path == "" {
		t.Skip("reference tracks missing")
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks, m.view = tracks, tracks
	m.status = Status{State: "playing", File: a.Path}
	m.prev = m.status
	m.loadArt(a.Path)
	um, _ := m.Update(statusMsg{st: Status{State: "playing", File: b.Path}})
	mm := um.(model)
	if mm.reveal != revealFrames {
		t.Fatalf("track-start must arm the wipe: reveal=%d", mm.reveal)
	}
	if strings.Contains(mm.artBlock, "▀") {
		t.Fatalf("wipe frame zero must start blank")
	}
}
