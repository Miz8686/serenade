package main

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	n, rest, ok := pickNext([]string{"/q1", "/q2"}, tr, "/a")
	if !ok || n != "/q1" || len(rest) != 1 || rest[0] != "/q2" {
		t.Fatalf("queue drain: %q %v %v", n, rest, ok)
	}
	// Empty queue falls back to library order.
	n, rest, ok = pickNext(nil, tr, "/a")
	if !ok || n != "/b" || len(rest) != 0 {
		t.Fatalf("fallback: %q %v %v", n, rest, ok)
	}
	// Empty everything: no advance.
	if _, _, ok = pickNext(nil, nil, "/a"); ok {
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
