package main

import "testing"

// The TUI's lyric bug class was manual terminal column counting:
// Devanagari spacing marks (Mc) counted 0 by runewidth while real
// terminals take a cell, fracturing wrap. GTK renders through
// Pango (native shaping, pixel ellipsize), so there is no width
// math to break — but parse + follow must still handle the script.
// This pins both on real Devanagari lines.
func TestLyricsDevanagari(t *testing.T) {
	lrc := `[ar:Albatross]
[00:01.00] तिमी भने
[00:05.00] खासेका तारा
[00:09.00] plain latin line
`
	lines := parseLRC(lrc)
	if len(lines) != 3 {
		t.Fatalf("parsed %d lines, want 3", len(lines))
	}
	if lines[0].text != "तिमी भने" || lines[1].text != "खासेका तारा" {
		t.Fatalf("Devanagari text mangled: %q %q", lines[0].text, lines[1].text)
	}
	// Follow: sounding line tracks position across scripts.
	if cur := currentLyric(lines, 0.5); cur != -1 {
		t.Fatalf("pre-first cur=%d want -1", cur)
	}
	if cur := currentLyric(lines, 6.0); cur != 1 {
		t.Fatalf("mid cur=%d want 1", cur)
	}
	if cur := currentLyric(lines, 30.0); cur != 2 {
		t.Fatalf("tail cur=%d want 2", cur)
	}
	from, to := lyricWindow(lines, true, 6.0, 0, lyrHeight)
	if from != 0 || to != 3 {
		t.Fatalf("window=%d..%d want 0..3", from, to)
	}
	// Plain (unsynced) Devanagari passes through verbatim.
	pl := plainLines("तिमी भने\n\nखासेका तारा\n")
	if len(pl) != 2 || pl[0].sec != -1 || pl[1].text != "खासेका तारा" {
		t.Fatalf("plainLines wrong: %+v", pl)
	}
}

func TestLyricCacheRoundTrip(t *testing.T) {
	t.Setenv("HOME", "/tmp/aser-lyrtest")
	lyricStore("A", "T", "Al", lyricCache{Found: true, Synced: "[00:01.00] hi"})
	c, ok := lyricLoad("A", "T", "Al")
	if !ok || !c.Found {
		t.Fatalf("cache miss: %+v %v", c, ok)
	}
	if l := parseLRC(c.Synced); len(l) != 1 || l[0].text != "hi" {
		t.Fatalf("cached parse wrong: %+v", l)
	}
	lyricStore("B", "T2", "", lyricCache{Found: false})
	c2, ok := lyricLoad("B", "T2", "")
	if !ok || c2.Found {
		t.Fatalf("tombstone must load as not-found: %+v %v", c2, ok)
	}
}

// TestLyricFitFallback pins the fit contract: nothing measured
// yet (headless, pre-map) means the full ceiling, never zero or
// negative — the box must not start collapsed.
func TestLyricFitFallback(t *testing.T) {
	a := &app{}
	if got := a.lyricFit(); got != lyrHeight {
		t.Fatalf("unmeasured fit = %d, want ceiling %d", got, lyrHeight)
	}
}
