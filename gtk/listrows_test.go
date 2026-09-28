package main

import "testing"

func TestListRows(t *testing.T) {
	view := []Track{
		{Path: "/a1", Artist: "Alpha", Title: "one"},
		{Path: "/a2", Artist: "Alpha", Title: "two"},
		{Path: "/b1", Artist: "Beta", Title: "one"},
		{Path: "/x", Title: "No Artist - Track"},
	}
	rows, labels := listRows(view)
	wantKinds := []rowKind{rowArtist, rowTrack, rowTrack, rowArtist, rowTrack}
	// empty-artist run sorts wherever it lands; here it's last: bare row
	if len(rows) != 6 {
		t.Fatalf("rows=%d want 6", len(rows))
	}
	for i, k := range wantKinds {
		if rows[i].kind != k {
			t.Fatalf("row %d kind=%d want %d", i, rows[i].kind, k)
		}
	}
	if labels[0] != "ALPHA" || labels[3] != "BETA" {
		t.Fatalf("headings not uppercased: %q %q", labels[0], labels[3])
	}
	if labels[1] != "one" || labels[5] != "No Artist - Track" {
		t.Fatalf("track labels wrong: %q %q", labels[1], labels[5])
	}
	if rows[5].kind != rowTrack || rows[5].idx != 3 {
		t.Fatalf("headless run must stay a plain track row")
	}
}

func TestPickNextPort(t *testing.T) {
	tr := []Track{{Path: "/a"}, {Path: "/b"}}
	var shuf shuffleState
	n, rest, ok := pickNext([]string{"/q"}, tr, "/a", &shuf)
	if !ok || n != "/q" || len(rest) != 0 {
		t.Fatalf("queue must win: %q", n)
	}
	if p, ok := prevTrackPath(tr, "/b"); !ok || p != "/a" {
		t.Fatalf("prev: %q", p)
	}
	if !isNaturalEnd(Status{State: "playing", File: "/a"}, Status{State: "stopped", File: "/a"}) {
		t.Fatalf("natural end not detected")
	}
}
