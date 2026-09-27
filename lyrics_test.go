package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestParseLRC(t *testing.T) {
	in := "[ti:Title]\n[ar:Artist]\n[offset:+500]\n[00:10.00]first\n[00:05.00][00:15.00]twice\n[by:nobody]\ngarbage\n[00:20]plain-sec\n"
	lines := parseLRC(in)
	if len(lines) != 4 {
		t.Fatalf("parsed %d lines, want 4", len(lines))
	}
	// sorted, offset applied: twice@5.5, first@10.5, twice@15.5, plain@20.5
	want := []struct {
		sec  float64
		text string
	}{{5.5, "twice"}, {10.5, "first"}, {15.5, "twice"}, {20.5, "plain-sec"}}
	for i, w := range want {
		if lines[i].sec != w.sec || lines[i].text != w.text {
			t.Fatalf("line %d = %.1f %q, want %.1f %q", i, lines[i].sec, lines[i].text, w.sec, w.text)
		}
	}
	if len(parseLRC("[ar:x]\nno stamps here\n")) != 0 {
		t.Fatalf("metadataless garbage must drop")
	}
}

func TestLyricWindow(t *testing.T) {
	var lines []lyricLine
	for i := 0; i < 20; i++ {
		lines = append(lines, lyricLine{sec: float64(i * 10), text: "l"})
	}
	// centers current line
	from, to := lyricWindow(lines, true, 105, 0, 5)
	if from != 8 || to != 13 {
		t.Fatalf("synced window = [%d,%d), want [8,13)", from, to)
	}
	// clamps at both ends
	if from, _ := lyricWindow(lines, true, 0, 0, 5); from != 0 {
		t.Fatalf("head clamp: %d", from)
	}
	if from, to := lyricWindow(lines, true, 999, 0, 5); from != 15 || to != 20 {
		t.Fatalf("tail clamp = [%d,%d)", from, to)
	}
	// plain windows by fraction
	if from, to := lyricWindow(lines, false, 0, 0.5, 5); from != 7 || to != 12 {
		t.Fatalf("plain window = [%d,%d)", from, to)
	}
	if from, to := lyricWindow(lines, false, 0, 0, 5); from != 0 || to != 5 {
		t.Fatalf("plain head = [%d,%d)", from, to)
	}
	if cur := currentLyric(lines, 105); cur != 10 {
		t.Fatalf("current = %d, want 10", cur)
	}
	if cur := currentLyric(lines, -1); cur != -1 {
		t.Fatalf("pre-first current = %d", cur)
	}
}

func TestLyricCache(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	artist, title, album := "Test Artist", "Test Title", "Test Album"
	lyricStore(artist, title, album, lyricCache{Found: true, Synced: "[00:01.00]hi", Plain: "hi"})
	c, ok := lyricLoad(artist, title, album)
	if !ok || !c.Found || c.Synced != "[00:01.00]hi" {
		t.Fatalf("round trip failed: %+v %v", c, ok)
	}
	lyricStore(artist, title, album, lyricCache{Found: false})
	c, ok = lyricLoad(artist, title, album)
	if !ok || c.Found {
		t.Fatalf("tombstone failed: %+v %v", c, ok)
	}
	os.Remove(filepath.Join(lyrDir(), lyricKey(artist, title, album)+".json"))
	if _, ok := lyricLoad(artist, title, album); ok {
		t.Fatalf("removed entry must miss")
	}
}

func TestRenderLyrics(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.lyrFile = "/x"
	var lines []lyricLine
	for i := 0; i < 10; i++ {
		lines = append(lines, lyricLine{sec: float64(i * 10), text: "line"})
	}
	m.lyrLines, m.lyrSynced = lines, true
	m.status = Status{Position: 35}
	out := m.renderLyrics(4)
	n := 0
	for _, r := range out {
		if r == '\n' {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("rendered %d rows for height 4", n)
	}
	if m.renderLyrics(1) != "" {
		t.Fatalf("height < 2 must stay empty")
	}
	m.lyrLines = nil
	m.lyrPending = ""
	if out := m.renderLyrics(4); out == "" {
		t.Fatalf("empty state must say so")
	}
}

func TestLyricMsgApply(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.lyrFile = "/b"
	um, _ := m.Update(lyricMsg{file: "/b", synced: "[00:10.00]hello"})
	mm := um.(model)
	if len(mm.lyrLines) != 1 || !mm.lyrSynced || mm.lyrLines[0].text != "hello" {
		t.Fatalf("fresh lyricMsg must apply: %+v", mm.lyrLines)
	}
	um, _ = mm.Update(lyricMsg{file: "/a", synced: "[00:10.00]stale"})
	if len(um.(model).lyrLines) != 1 || um.(model).lyrLines[0].text != "hello" {
		t.Fatalf("stale lyricMsg must drop")
	}
}

func TestLyricHighlight(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.applyAccent("#fd002a", "#8c7a86")
	m.lyrFile = "/x"
	m.lyrLines = []lyricLine{{10, "early"}, {20, "now"}, {30, "later"}}
	m.lyrSynced = true
	m.status = Status{Position: 25}
	out := m.renderLyrics(3)
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[1], "48;2;253;0;42m") {
		t.Fatalf("current line must carry the selection pill: %q", lines[1])
	}
	if strings.Contains(lines[0], "253;0;42") || strings.Contains(lines[2], "253;0;42") {
		t.Fatalf("only the current line accents")
	}
}

func TestFrameHeightWithLyrics(t *testing.T) {
	// Regression: Devanagari lyric lines once word-wrapped in the
	// frame renderer (ruler disagreement), fracturing the view with
	// phantom rows. The frame must stay exactly height rows.
	os.Setenv("HOME", "/home/miz")
	c, ok := lyricLoad("Albatross", "Khaseka Tara", "Jo Jas Sanga Sambandhit Chha - EP")
	if !ok || !c.Found {
		t.Skip("lyric cache missing")
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{{Path: "/x", Artist: "A", Title: "T"}}
	m.view = m.tracks
	m.indexing = false
	m.lyrFile = "/x"
	m.lyrLines = plainLines(c.Plain)
	m.artImg = stripeImage(144, 144, 6, 0.8, 0.8)
	m.artBlock = m.renderArt()
	m.syncBgGrid()
	m.layoutPanes()
	out := m.View()
	if n := strings.Count(out, "\n") + 1; n != 39 {
		t.Fatalf("view is %d rows, want 39", n)
	}
}

func TestLyricWidth(t *testing.T) {
	if lyricWidth("hello") != 5 {
		t.Fatalf("ascii")
	}
	if lyricWidth("日本語") != 6 {
		t.Fatalf("cjk")
	}
	// Devanagari vowel signs (Mc, spacing) take a cell each in real
	// terminals; runewidth zeroes them and lets lines eat the frame.
	marks := 0
	for _, r := range "ोे" {
		if !isNonspacing(r) {
			marks++
		}
	}
	if marks != 2 {
		t.Fatalf("Mc marks must count, got %d", marks)
	}
	// ख(1) ्(Mn→0) स(1) े(Mc→1) क(1) ा(Mc→1) = 5 cells
	if got := lyricWidth("खसेका"); got != 5 {
		t.Fatalf("khaseka width=%d want 5", got)
	}
	long := "खसेका तारा गन्दै तिमीलाई नै मागी बसेँ आज फेरि अतिरिक्त शब्दहरू"
	tr := lyricTruncate(long, 65)
	if lyricWidth(tr) > 65 {
		t.Fatalf("truncated width=%d over 65", lyricWidth(tr))
	}
	if lyricTruncate("short", 65) != "short" {
		t.Fatalf("short lines pass through")
	}
}

func TestContentWidthWithLyrics(t *testing.T) {
	// Every content row must fit contentW cells on a Mc=1 ruler
	// (real terminals) or text eats the frame edge.
	os.Setenv("HOME", "/home/miz")
	c, ok := lyricLoad("Albatross", "Khaseka Tara", "Jo Jas Sanga Sambandhit Chha - EP")
	if !ok || !c.Found {
		t.Skip("lyric cache missing")
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.tracks = []Track{{Path: "/x", Artist: "A", Title: "T"}}
	m.view = m.tracks
	m.indexing = false
	m.lyrFile = "/x"
	m.lyrLines = plainLines(c.Plain)
	m.artImg = stripeImage(144, 144, 6, 0.8, 0.8)
	m.artBlock = m.renderArt()
	m.syncBgGrid()
	m.layoutPanes()
	m.status = Status{State: "playing", File: "/x", Position: 30, Duration: 180}
	out := m.View()
	for i, ln := range strings.Split(out, "\n") {
		if w := lyricWidth(stripANSI(ln)); w > 167 {
			s := stripANSI(ln)
			t.Fatalf("row %d is %d cells wide runes=%d\n%q", i, w, len([]rune(s)), s)
		}
	}
}
