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

func TestNoTrailingBG(t *testing.T) {
	// The brief's regression net: background-colored styles must
	// never extend past the last visible glyph — on headings,
	// pills, muted rows, or lyric lines, in any script. Counts
	// bg-carrying cells after the final glyph.
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	buildBaseStyles()
	stray := func(rendered string) int {
		type cell struct {
			ch rune
			bg bool
		}
		var cells []cell
		bg := false
		rs := []rune(rendered)
		for i := 0; i < len(rs); {
			if rs[i] == '\x1b' && i+1 < len(rs) && rs[i+1] == '[' {
				j := i + 2
				for j < len(rs) && !(rs[j] >= '@' && rs[j] <= '~') {
					j++
				}
				if j < len(rs) {
					j++
				}
				if strings.HasSuffix(string(rs[i:j]), "m") {
					bg = sgrBg(string(rs[i:j]), bg)
				}
				i = j
				continue
			}
			cells = append(cells, cell{rs[i], bg})
			i++
		}
		last := -1
		for k, c := range cells {
			if c.ch != ' ' {
				last = k
			}
		}
		n := 0
		for k := last + 1; k < len(cells); k++ {
			if cells[k].bg {
				n++
			}
		}
		return n
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	pad := func(s string) string {
		if d := m.listPaneW() - lyricWidth(stripANSI(s)); d > 0 {
			return s + strings.Repeat(" ", d)
		}
		return s
	}
	cases := map[string]string{
		"heading": pad(styleArtist.Render("काठमाडौं एल्बम")),
		"pill":    styleSelected.Render("test"),
		"muted":   styleMuted.Render("album line here"),
		"lyric":   styleMuted.Render("खसेका तारा गन्दै"),
		"lyrpill": styleSelected.Render("खसेका तारा गन्दै"),
	}
	for name, out := range cases {
		if n := stray(out); n > 0 {
			t.Fatalf("%s: %d bg cells past last glyph", name, n)
		}
	}
}

func TestMergeLyricResponses(t *testing.T) {
	str := func(s string) *string { return &s }
	// exact synced wins outright
	s, p := mergeLyricResponses(lrclibResp{SyncedLyrics: str("s1"), PlainLyrics: str("p1")}, lrclibResp{SyncedLyrics: str("s2")})
	if s != "s1" || p != "p1" {
		t.Fatalf("exact must win: %q %q", s, p)
	}
	// plain-only exact + synced bare = the Khaseka case
	s, p = mergeLyricResponses(lrclibResp{PlainLyrics: str("p1")}, lrclibResp{SyncedLyrics: str("s2"), PlainLyrics: str("p2")})
	if s != "s2" || p != "p1" {
		t.Fatalf("bare synced must upgrade: %q %q", s, p)
	}
	// both empty stays empty (caller tombstones)
	s, p = mergeLyricResponses(lrclibResp{}, lrclibResp{})
	if s != "" || p != "" {
		t.Fatalf("empty must stay empty")
	}
}

func TestFullViewNoTrailingBG(t *testing.T) {
	// Closes the audit-vs-real gap: the isolated-row audit can't
	// see join padding, painter flush, or frame assembly. This one
	// scans full-View rows (Devanagari synced lyrics live) and
	// asserts no bg survives past the last visible glyph — except
	// the sounding-line pill, which is full-width by design.
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	buildBaseStyles()
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.tracks, m.view = tracks, tracks
	m.indexing = false
	m.width, m.height = 167, 39
	m.layoutPanes()
	var k *Track
	for i := range tracks {
		if tracks[i].Title == "Khaseka Tara" {
			k = &tracks[i]
		}
	}
	m.status = Status{State: "playing", File: k.Path, Artist: k.Artist, Title: k.Title, Album: k.Album, Duration: 301, Position: 74}
	m.loadArt(k.Path)
	m.followPlaying(k.Path)
	m.loadLyrics(k.Path)
	m.syncDetail()
	m.clampCursor()
	curText := ""
	if m.lyrSynced {
		if cur := currentLyric(m.lyrLines, 74); cur >= 0 {
			curText = m.lyrLines[cur].text
		}
	}
	for i, ln := range strings.Split(m.View(), "\n") {
		if curText != "" && strings.Contains(stripANSI(ln), curText) {
			continue // the pill owns its full width by design
		}
		type cell struct {
			ch rune
			bg bool
		}
		var cells []cell
		bg := false
		rs := []rune(ln)
		for j := 0; j < len(rs); {
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '[' {
				e := j + 2
				for e < len(rs) && !(rs[e] >= '@' && rs[e] <= '~') {
					e++
				}
				if e < len(rs) {
					e++
				}
				if strings.HasSuffix(string(rs[j:e]), "m") {
					bg = sgrBg(string(rs[j:e]), bg)
				}
				j = e
				continue
			}
			cells = append(cells, cell{rs[j], bg})
			j++
		}
		last := -1
		for k2, c := range cells {
			if c.ch != ' ' {
				last = k2
			}
		}
		for k2 := last + 1; k2 < len(cells); k2++ {
			if cells[k2].bg {
				t.Fatalf("row %d: bg past last glyph", i)
			}
		}
	}
}

func TestNoUnpaintedCells(t *testing.T) {
	// Inverse of the trailing-bg audit: with the backdrop grid
	// active, EVERY cell must carry a background. Simulates the
	// terminal: 48-colors set state, resets (0/empty/49) clear it.
	// This is the test that would have caught the dedup hole —
	// trailing resets left padding unpainted in real terminals
	// while the Ascii-profile screenshots looked perfect.
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	buildBaseStyles()
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.tracks, m.view = tracks, tracks
	m.indexing = false
	m.width, m.height = 167, 39
	m.layoutPanes()
	var k *Track
	for i := range tracks {
		if tracks[i].Title == "Khaseka Tara" {
			k = &tracks[i]
		}
	}
	m.status = Status{State: "playing", File: k.Path, Artist: k.Artist, Title: k.Title, Album: k.Album, Duration: 301, Position: 74}
	m.loadArt(k.Path)
	m.followPlaying(k.Path)
	m.loadLyrics(k.Path)
	m.syncDetail()
	m.clampCursor()
	if len(m.bgGrid) == 0 {
		t.Fatalf("no backdrop grid")
	}
	for i, ln := range strings.Split(m.View(), "\n") {
		bg := false
		rs := []rune(ln)
		for j := 0; j < len(rs); {
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '[' {
				e := j + 2
				for e < len(rs) && !(rs[e] >= '@' && rs[e] <= '~') {
					e++
				}
				if e < len(rs) {
					e++
				}
				bg = sgrBg(string(rs[j:e]), bg)
				j = e
				continue
			}
			if !bg {
				t.Fatalf("row %d: unpainted cell %q", i, string(rs[j]))
			}
			j++
		}
	}
}
