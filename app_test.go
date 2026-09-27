package main

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	playing := stylePlaying.Render("▶ artist - title")
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
