package main

// lyrics.go — lyrics for the dead space under the art panel.
//
// Source precedence per track: embedded USLT/SYLT tags → lrclib.net
// (free, no key, synced + plain) with on-disk cache → quiet empty
// state. Network happens exactly once per track (async tea.Cmd,
// in-flight guard); misses cache a tombstone so offline plays stay
// silent instead of retrying every poll.
//
// Rendering (ui.go) fills EXACTLY the leftover right-column rows —
// no new layout, no reserved block. Synced lines auto-follow
// position; plain lines window by progress fraction. Zero new keys.

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// lyricLine is one display line: sec < 0 means un timed (plain).
type lyricLine struct {
	sec  float64
	text string
}

// parseLRC parses [mm:ss.xx] synced lines. Skips metadata tags
// ([ar:], [ti:], [al:], [by:], [length:]), applies [offset:±ms],
// expands multi-stamp lines, sorts by time. Garbage lines drop.
func parseLRC(s string) []lyricLine {
	stamp := func(p string) (float64, bool) {
		// mm:ss(.xx) — also tolerates hh:mm:ss. Permissive on range;
		// garbage fails the number parses below.
		parts := strings.Split(p, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return 0, false
		}
		sec, err := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-1]), 64)
		if err != nil || sec < 0 {
			return 0, false
		}
		total := sec
		mult := 60.0
		for i := len(parts) - 2; i >= 0; i-- {
			v, err := strconv.Atoi(strings.TrimSpace(parts[i]))
			if err != nil || v < 0 {
				return 0, false
			}
			total += float64(v) * mult
			mult *= 60
		}
		return total, true
	}
	var offset float64
	var out []lyricLine
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimRight(raw, "\r ")
		if line == "" {
			continue
		}
		// collect leading [..] tags
		var stamps []float64
		rest := line
		meta := false
		for strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			if end < 0 {
				break
			}
			tag := rest[1:end]
			rest = rest[end+1:]
			lower := strings.ToLower(tag)
			switch {
			case strings.HasPrefix(lower, "offset:"):
				if v, err := strconv.Atoi(strings.TrimSpace(tag[7:])); err == nil {
					offset = float64(v) / 1000
				}
				meta = true
			case strings.HasPrefix(lower, "ar:") || strings.HasPrefix(lower, "ti:") ||
				strings.HasPrefix(lower, "al:") || strings.HasPrefix(lower, "by:") ||
				strings.HasPrefix(lower, "length:"):
				meta = true
			default:
				if sec, ok := stamp(tag); ok {
					stamps = append(stamps, sec)
				} else {
					meta = true
				}
			}
		}
		if meta && len(stamps) == 0 {
			continue
		}
		text := strings.TrimSpace(rest)
		if text == "" {
			continue
		}
		for _, st := range stamps {
			out = append(out, lyricLine{sec: st + offset, text: text})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sec < out[j].sec })
	return out
}

// lyricWidth counts terminal cells the way a dumb terminal does:
// one cell per codepoint except zero-width joiners and nonspacing
// marks. Spacing marks (Devanagari vowel signs et al., Mc) take a
// cell — runewidth and x/ansi both count them 0, which undercounts
// real terminals and lets lyric lines eat the frame edge. Latin and
// CJK measure identically to runewidth; only Mc differs.
func lyricWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\u200d' || r == '\ufe0f' {
			continue
		}
		if isNonspacing(r) {
			continue
		}
		// East Asian wide/fullwidth stay 2; everything else 1.
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isNonspacing(r rune) bool {
	// Mn (nonspacing) and Me (enclosing) combine into the previous
	// cell; Cf format chars are invisible. Mc (spacing) is NOT
	// here on purpose — see lyricWidth.
	return (r >= 0x300 && r <= 0x36F) || (r >= 0x1AB0 && r <= 0x1AFF) ||
		(r >= 0x1DC0 && r <= 0x1DFF) || (r >= 0x20D0 && r <= 0x20FF) ||
		(r >= 0xFE20 && r <= 0xFE2F) || r == 0x200C || r == 0x200D ||
		(r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0100 && r <= 0xE01EF) ||
		(r >= 0x0483 && r <= 0x0489) || (r >= 0x0591 && r <= 0x05BD) ||
		(r >= 0x0610 && r <= 0x061A) || (r >= 0x0646 && r <= 0x0652) ||
		(r >= 0x0656 && r <= 0x065F) || (r >= 0x0670 && r <= 0x0670) ||
		(r >= 0x06D6 && r <= 0x06DC)
}

func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) || r == 0x2329 || r == 0x232A ||
		(r >= 0x2E80 && r <= 0x303E) || (r >= 0x3041 && r <= 0x33FF) ||
		(r >= 0x3400 && r <= 0x4DBF) || (r >= 0x4E00 && r <= 0xA4CF) ||
		(r >= 0xA960 && r <= 0xA97C) || (r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE10 && r <= 0xFE19) ||
		(r >= 0xFE30 && r <= 0xFE4F) || (r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) || (r >= 0x20000 && r <= 0x3FFFD)
}

// lyricTruncate cuts s to w terminal cells, appending … on cut.
// Dual cap: cell width AND rune count. Unshaped terminals give
// every codepoint its own cell, so a 65-"cell" Devanagari line can
// still be 80+ cells there and eat the frame edge; the rune cap
// bounds that worst case while the cell cap handles CJK/emoji.
func lyricTruncate(s string, w int) string {
	if lyricWidth(s) <= w && len([]rune(s)) <= w {
		return s
	}
	var b strings.Builder
	used, runes := 0, 0
	for _, r := range s {
		var rw int
		if isNonspacing(r) || r == '\u200d' || r == '\ufe0f' {
			rw = 0
		} else if isWide(r) {
			rw = 2
		} else {
			rw = 1
		}
		if used+rw > w-1 || runes+1 > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
		runes++
	}
	return b.String() + "\u2026"
}

// lyricCellWidth is the per-rune terminal cell count: wide 2,
// zero-width joiners and nonspacing marks 0, everything else
// (including spacing marks) 1. Shared by the backdrop painter so
// grid columns never drift on non-Latin rows.
func lyricCellWidth(r rune) int {
	if r == '\u200d' || r == '\ufe0f' {
		return 0
	}
	if isNonspacing(r) {
		return 0
	}
	if isWide(r) {
		return 2
	}
	return 1
}

// lyricPadRight pads s with spaces to exactly w lyricWidth cells so
// downstream joins (which measure with a different ruler) add
// nothing and can't overshoot the column.
func lyricPadRight(s string, w int) string {
	if d := w - lyricWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// plainLines splits unsynced text into display lines.
func plainLines(s string) []lyricLine {
	var out []lyricLine
	for _, raw := range strings.Split(s, "\n") {
		if t := strings.TrimRight(raw, "\r "); t != "" {
			out = append(out, lyricLine{sec: -1, text: t})
		}
	}
	return out
}

// lyricWindow returns the [from,to) line range filling height rows:
// synced centers the current line at posSec, plain windows by
// fraction f in [0,1].
func lyricWindow(lines []lyricLine, synced bool, posSec, frac float64, height int) (int, int) {
	n := len(lines)
	if n == 0 || height <= 0 {
		return 0, 0
	}
	if height >= n {
		return 0, n
	}
	var start int
	if synced {
		cur := 0
		for i, l := range lines {
			if l.sec <= posSec {
				cur = i
			} else {
				break
			}
		}
		start = cur - height/2
	} else {
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		start = int(frac * float64(n-height))
	}
	if start < 0 {
		start = 0
	}
	if start > n-height {
		start = n - height
	}
	return start, start + height
}

// currentLyric is the synced line index at posSec, or -1.
func currentLyric(lines []lyricLine, posSec float64) int {
	cur := -1
	for i, l := range lines {
		if l.sec < 0 {
			continue
		}
		if l.sec <= posSec {
			cur = i
		} else {
			break
		}
	}
	return cur
}

func lyrDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "serenade", "lyrics")
}

// lyricKey pins the cache entry to the track's identity (tags —
// duration deliberately excluded: the poll may not know it yet on
// the first transition, and remasters share lyrics anyway).
func lyricKey(artist, title, album string) string {
	h := sha1.Sum([]byte(artist + "\x00" + title + "\x00" + album))
	return fmt.Sprintf("%x", h)
}

type lyricCache struct {
	Found  bool   `json:"found"`
	Synced string `json:"synced,omitempty"`
	Plain  string `json:"plain,omitempty"`
}

func lyricLoad(artist, title, album string) (lyricCache, bool) {
	var c lyricCache
	raw, err := os.ReadFile(filepath.Join(lyrDir(), lyricKey(artist, title, album)+".json"))
	if err != nil {
		return c, false
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, false
	}
	return c, true
}

func lyricStore(artist, title, album string, c lyricCache) {
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.MkdirAll(lyrDir(), 0o755)
	_ = os.WriteFile(filepath.Join(lyrDir(), lyricKey(artist, title, album)+".json"), raw, 0o644)
}

// lrclib entry: null lyrics arrive as nil, never "".
type lrclibResp struct {
	SyncedLyrics *string `json:"syncedLyrics"`
	PlainLyrics  *string `json:"plainLyrics"`
}

// fetchLyrics resolves lyrics with an album-qualified query first,
// then retries bare (artist+title): lrclib sometimes files the
// synced variant under a different entry than the exact match
// (verified: album-qualified Khaseka Tara returns plain-only while
// the bare query has full synced lines). notFound is true only
// when the track genuinely has nothing — transient failures (5xx,
// timeouts, decode) must NOT tombstone, or one bad network day
// silences tracks forever.
func fetchLyrics(artist, title, album string, duration int) (synced, plain string, notFound bool, err error) {
	get := func(q url.Values) (lrclibResp, int, error) {
		var lr lrclibResp
		req, err := http.NewRequest("GET", "https://lrclib.net/api/get?"+q.Encode(), nil)
		if err != nil {
			return lr, 0, err
		}
		req.Header.Set("User-Agent", "serenade/1.0")
		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return lr, 0, err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 {
			return lr, 404, nil
		}
		if resp.StatusCode != 200 {
			return lr, resp.StatusCode, fmt.Errorf("lrclib %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			return lr, resp.StatusCode, err
		}
		return lr, resp.StatusCode, nil
	}
	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", title)
	q.Set("album_name", album)
	if duration > 0 {
		q.Set("duration", fmt.Sprint(duration))
	}
	lr, code, err := get(q)
	if err != nil {
		if code == 404 {
			return "", "", true, fmt.Errorf("no lyrics")
		}
		return "", "", false, err
	}
	synced, plain = mergeLyricResponses(lr, lrclibResp{})
	if synced == "" {
		// Exact match is plain-only: retry bare before settling.
		q2 := url.Values{}
		q2.Set("artist_name", artist)
		q2.Set("track_name", title)
		if lr2, _, err := get(q2); err == nil {
			synced, plain = mergeLyricResponses(lr, lr2)
		}
	}
	if synced == "" && plain == "" {
		return "", "", true, fmt.Errorf("no lyrics")
	}
	return synced, plain, false, nil
}

// mergeLyricResponses prefers synced lines wherever found across
// the exact and bare matches; plain fills from either. Pure for
// testing the retry decision without network.
func mergeLyricResponses(exact, bare lrclibResp) (synced, plain string) {
	if exact.SyncedLyrics != nil {
		synced = *exact.SyncedLyrics
	}
	if exact.PlainLyrics != nil {
		plain = *exact.PlainLyrics
	}
	if synced == "" && bare.SyncedLyrics != nil {
		synced = *bare.SyncedLyrics
	}
	if plain == "" && bare.PlainLyrics != nil {
		plain = *bare.PlainLyrics
	}
	return synced, plain
}

// lyricMsg carries an async fetch result. Stale arrivals (track
// moved on) are dropped by the file check at the handler.
type lyricMsg struct {
	file   string
	synced string
	plain  string
	err    error
}

func fetchLyricsCmd(file, artist, title, album string, duration int) tea.Cmd {
	return func() tea.Msg {
		synced, plain, notFound, err := fetchLyrics(artist, title, album, duration)
		if err == nil {
			lyricStore(artist, title, album, lyricCache{Found: true, Synced: synced, Plain: plain})
		} else if notFound {
			lyricStore(artist, title, album, lyricCache{Found: false})
		}
		return lyricMsg{file, synced, plain, err}
	}
}
