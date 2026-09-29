package main

import (
	"strings"
	"testing"
)

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
	if labels[0] != "Alpha" || labels[3] != "Beta" {
		t.Fatalf("headings must keep source casing: %q %q", labels[0], labels[3])
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

func TestPickText(t *testing.T) {
	if pickText("#D9A44C") != "#161310" {
		t.Fatalf("brass needs dark text")
	}
	if pickText("#7a1f1f") != "#EDE0C8" {
		t.Fatalf("deep red needs paper text")
	}
	if pickText("#161310") != "#EDE0C8" {
		t.Fatalf("ink needs paper text")
	}
}

func TestAccentCSS(t *testing.T) {
	css := accentCSS("#fd002a")
	for _, want := range []string{
		".tracklist row:selected",
		"background: #fd002a;",
		".transport-btn:hover",
		".play-primary:active",
		".seek-row scale highlight",
		"alpha(#fd002a, 0.22)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("accent CSS missing %q", want)
		}
	}
	if strings.Contains(css, "#D9A44C") {
		t.Fatalf("override must not hardcode brand")
	}
}

func TestAlbumLabel(t *testing.T) {
	mk := func() ([]Track, []listRow) {
		view := []Track{
			{Path: "/a1", Artist: "Alpha", Album: "One", Title: "t1"},
			{Path: "/a2", Artist: "Alpha", Album: "One", Title: "t2"},
			{Path: "/a3", Artist: "Alpha", Album: "Two", Title: "t3"},
			{Path: "/s1", Artist: "Solo", Album: "Solo", Title: "Solo"},
			{Path: "/b1", Artist: "Beta", Album: "", Title: "t4"},
		}
		rows, _ := listRows(view)
		return view, rows
	}
	view, rows := mk()
	// rows: H(a1) T(a1) T(a2) T(a3) H(s1) T(s1) H(b1) T(b1)
	byIdx := map[int]int{}
	for pos, r := range rows {
		if r.kind == rowTrack {
			byIdx[r.idx] = pos
		}
	}
	cases := []struct {
		idx  int
		want string
	}{
		{0, "One"}, // first of run
		{1, ""},    // repeat suppressed
		{2, "Two"}, // new album in run
		{3, ""},    // title==album (singles case)
		{4, ""},    // empty album
	}
	for _, c := range cases {
		if got := albumLabel(view, rows, byIdx[c.idx], view[c.idx]); got != c.want {
			t.Fatalf("idx %d: got %q want %q", c.idx, got, c.want)
		}
	}
}
