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

// fetchLyrics asks lrclib once. No key, 12s cap, honest UA.
func fetchLyrics(artist, title, album string, duration int) (synced, plain string, err error) {
	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", title)
	q.Set("album_name", album)
	if duration > 0 {
		q.Set("duration", fmt.Sprint(duration))
	}
	req, err := http.NewRequest("GET", "https://lrclib.net/api/get?"+q.Encode(), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "serenade/1.0")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("lrclib %d", resp.StatusCode)
	}
	var lr lrclibResp
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", "", err
	}
	if lr.SyncedLyrics != nil {
		synced = *lr.SyncedLyrics
	}
	if lr.PlainLyrics != nil {
		plain = *lr.PlainLyrics
	}
	if synced == "" && plain == "" {
		return "", "", fmt.Errorf("no lyrics")
	}
	return synced, plain, nil
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
		synced, plain, err := fetchLyrics(artist, title, album, duration)
		if err == nil {
			lyricStore(artist, title, album, lyricCache{Found: true, Synced: synced, Plain: plain})
		} else {
			lyricStore(artist, title, album, lyricCache{Found: false})
		}
		return lyricMsg{file, synced, plain, err}
	}
}
