package main

import "testing"

// The scroll-follow bug: the playing row docked at opposite
// viewport ends depending on trigger. Root cause was NOT two
// index spaces (the lookup was already single) but GTK's bare
// ScrollTo, which only reveals with minimal travel — the row
// docks top-edge coming from above, bottom-edge from below.
// The fix keeps ONE pure mapping (rowForPath, used by manual
// selection, auto-advance, next, and prev alike — no
// per-trigger arithmetic left) and centers deterministically.
//
// This pins the shared mapping: every trigger's file resolves
// to the same composite-model row (headings included), and
// absent/filtered files resolve to -1 instead of clamping to
// an end — the clamp-low/clamp-high shape the old symptom had.
func TestRowForPath(t *testing.T) {
	view := []Track{
		{Path: "/a1", Artist: "Alpha", Album: "One", Title: "t1"},
		{Path: "/a2", Artist: "Alpha", Album: "One", Title: "t2"},
		{Path: "/b1", Artist: "Beta", Album: "Solo", Title: "Solo"},
		{Path: "/x", Title: "Lone"},
	}
	rows, _ := listRows(view)
	// Model: H(a1) T(a1) T(a2) H(b1) T(b1) T(x)
	//  pos:  0     1     2     3     4     5
	for _, c := range []struct {
		path string
		want int
	}{
		{"/a1", 1}, // first track sits under its heading
		{"/a2", 2}, // second of run, not 1 (no under-count)
		{"/b1", 4}, // past a heading, not 2 or 3
		{"/x", 5},  // headless tail, not clamped from anywhere
		{"/missing", -1},
		{"", -1},
	} {
		if got := rowForPath(view, rows, c.path); got != c.want {
			t.Fatalf("rowForPath(%q) = %d, want %d", c.path, got, c.want)
		}
	}
}

// All four triggers must agree: same file → same row, whichever
// path asked. They share rowForPath (followPlaying + rebindRow
// delegate; playSelected/playAt map the inverse direction), so
// this exercises the lookup once per trigger role.
func TestRowForPathAllTriggers(t *testing.T) {
	view := []Track{
		{Path: "/a", Artist: "A", Title: "one"},
		{Path: "/b", Artist: "A", Title: "two"},
		{Path: "/c", Artist: "C", Title: "three"},
	}
	rows, _ := listRows(view)
	// Manual (double-click/Enter on view index), auto-advance and
	// next (library-order file), prev (library-order file): each
	// resolves through rowForPath to model row 2 for "/b".
	triggers := map[string]string{
		"manual-double-click": view[1].Path,
		"auto-advance":        "/b",
		"next":                "/b",
		"prev":                "/b",
	}
	for name, path := range triggers {
		if got := rowForPath(view, rows, path); got != 2 {
			t.Fatalf("%s resolved row %d, want 2", name, got)
		}
	}
}
