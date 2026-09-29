package main

import "testing"

// The TUI got bitten by a transition-frame bug under rapid
// track changes; the crossfade guards that edge with a generation
// counter. Pin the guard pure: only the newest fade stays live.
func TestFadeGen(t *testing.T) {
	var g fadeGen
	first := g.next()
	if !g.live(first) {
		t.Fatalf("fresh gen must be live")
	}
	second := g.next()
	if g.live(first) {
		t.Fatalf("superseded gen must be dead")
	}
	if !g.live(second) {
		t.Fatalf("newest gen must be live")
	}
	for i := 0; i < 50; i++ {
		g.next()
	}
	if g.live(second) {
		t.Fatalf("old gen must stay dead after many skips")
	}
}

// Headless loadArt: missing art resolves through the nil-widget
// path without touching GTK — rapid double-call only bumps the
// generation, proving stale swaps can't interleave.
func TestLoadArtHeadlessRapid(t *testing.T) {
	a := &app{cfg: defaultConfig()}
	a.loadArt("/nonexistent/track.flac")
	a.loadArt("/nonexistent/other.flac")
	if a.fGen.cur != 2 {
		t.Fatalf("two loadArts must bump gen twice, got %d", a.fGen.cur)
	}
	if a.bgTex != nil {
		t.Fatalf("missing art must leave backdrop clear")
	}
}
