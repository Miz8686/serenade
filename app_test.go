package main

import (
	"os"
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
