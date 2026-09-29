package main

// lyricsview.go — lyrics box under the album line.
//
// Pipeline (lyrics_pipe.go) is the TUI's as-is: embedded tags →
// disk cache (+ tombstones) → one async lrclib fetch per track.
// Only the trigger differs: a goroutine + IdleAdd instead of a
// tea.Cmd, with the same stale-arrival file check.
//
// View is new: a fixed 6-line box. Synced lines auto-follow via
// currentLyric; the sounding line renders paper-bold, the rest
// muted — fixed tokens, never the per-track accent (ceiling).
//
// Devanagari note: the TUI bug class here was manual terminal
// column counting (Mc marks counted 0 by runewidth). GTK has no
// equivalent: text goes to Pango labels verbatim, shaped natively
// and ellipsized by pixel. No width math anywhere in this file by
// design; the unit test pins Devanagari parse + follow.

import (
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// lyrHeight is the fixed lyric box size: compact, art stays owner.
const lyrHeight = 6

// buildLyrics creates the fixed lyric box. Called once from the
// Now Playing build, after the album label.
func (a *app) buildLyrics(parent *gtk.Box) {
	a.lyrBox = gtk.NewBox(gtk.OrientationVertical, 1)
	a.lyrBox.SetMarginTop(8)
	a.lyrBox.AddCSSClass("lyrics")
	parent.Append(a.lyrBox)
	a.lyrLabels = make([]*gtk.Label, 0, lyrHeight)
	for i := 0; i < lyrHeight; i++ {
		l := gtk.NewLabel("")
		l.SetXAlign(0)
		l.SetEllipsize(pango.EllipsizeEnd)
		l.SetSingleLineMode(true)
		l.AddCSSClass("lyric-line")
		a.lyrBox.Append(l)
		a.lyrLabels = append(a.lyrLabels, l)
	}
	a.lyrBox.SetVisible(false)
}

// resolveLyrics mirrors the TUI's loadLyrics precedence for file:
// embedded tags, then disk cache (tombstone = stay quiet), then
// one async fetch. Inline resolutions apply immediately; the
// fetch applies via IdleAdd only if still current.
func (a *app) resolveLyrics(file string) {
	a.lyrFile = file
	a.lyrLines = nil
	a.lyrSynced = false
	if file == "" {
		a.updateLyrics()
		return
	}
	artist, title, album := "", baseName(file), ""
	for i := range a.tracks {
		if a.tracks[i].Path == file {
			artist, title, album = a.tracks[i].Artist, a.tracks[i].Title, a.tracks[i].Album
			if a.tracks[i].Lyrics != "" {
				if l := parseLRC(a.tracks[i].Lyrics); len(l) > 0 {
					a.lyrLines, a.lyrSynced = l, true
				} else {
					a.lyrLines = plainLines(a.tracks[i].Lyrics)
				}
				a.updateLyrics()
				return
			}
			break
		}
	}
	dur := a.status.Duration
	if c, ok := lyricLoad(artist, title, album); ok {
		if !c.Found {
			a.updateLyrics()
			return // tombstone: stay quiet
		}
		if l := parseLRC(c.Synced); len(l) > 0 {
			a.lyrLines, a.lyrSynced = l, true
		} else {
			a.lyrLines = plainLines(c.Plain)
		}
		a.updateLyrics()
		return
	}
	if a.lyrFlight == file {
		return
	}
	a.lyrFlight = file
	go func() {
		synced, plain, notFound, err := fetchLyrics(artist, title, album, dur)
		if err == nil {
			lyricStore(artist, title, album, lyricCache{Found: true, Synced: synced, Plain: plain})
		} else if notFound {
			lyricStore(artist, title, album, lyricCache{Found: false})
		}
		glib.IdleAdd(func() bool {
			if a.lyrFile != file {
				return false // stale arrival: track moved on
			}
			a.lyrFlight = ""
			if err == nil {
				if l := parseLRC(synced); len(l) > 0 {
					a.lyrLines, a.lyrSynced = l, true
				} else {
					a.lyrLines = plainLines(plain)
				}
				a.updateLyrics()
			}
			return false
		})
	}()
}

// updateLyrics windows the resolved lines around the current
// position and paints the box. Empty lines hide the box outright
// — no reserved blank space.
func (a *app) updateLyrics() {
	if a.lyrBox == nil {
		return
	}
	if len(a.lyrLines) == 0 {
		a.lyrBox.SetVisible(false)
		return
	}
	a.lyrBox.SetVisible(true)
	var pos, frac float64
	if a.status.Duration > 0 {
		pos = float64(a.status.Position)
		frac = float64(a.status.Position) / float64(a.status.Duration)
	}
	from, to := lyricWindow(a.lyrLines, a.lyrSynced, pos, frac, lyrHeight)
	cur := -1
	if a.lyrSynced {
		cur = currentLyric(a.lyrLines, pos)
	}
	for i, l := range a.lyrLabels {
		li := from + i
		if li >= to {
			l.SetText("")
			l.RemoveCSSClass("lyric-current")
			continue
		}
		l.SetText(a.lyrLines[li].text)
		if li == cur {
			l.AddCSSClass("lyric-current")
		} else {
			l.RemoveCSSClass("lyric-current")
		}
	}
}
