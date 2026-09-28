package main

// nowplaying.go — right panel: art (GdkTexture), type hierarchy,
// transport, seek. Poll loop mirrors the TUI statusMsg choke points:
// file change → art + accents + follow; natural end → advance.

import (
	"bytes"
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

func (a *app) buildNowPlaying(right *gtk.Box) {
	a.art = gtk.NewPicture()
	a.art.SetSizeRequest(320, 320)
	a.art.AddCSSClass("cover")
	right.Append(a.art)

	a.noArt = gtk.NewLabel("no cover art")
	a.noArt.AddCSSClass("dim")
	a.noArt.SetVisible(false)
	right.Append(a.noArt)

	a.artistL = gtk.NewLabel("")
	a.artistL.SetXAlign(0)
	a.artistL.AddCSSClass("np-artist")
	right.Append(a.artistL)

	a.titleL = gtk.NewLabel("")
	a.titleL.SetXAlign(0)
	a.titleL.SetEllipsize(pango.EllipsizeEnd)
	a.titleL.AddCSSClass("np-title")
	right.Append(a.titleL)

	a.albumL = gtk.NewLabel("")
	a.albumL.SetXAlign(0)
	a.albumL.SetEllipsize(pango.EllipsizeEnd)
	a.albumL.AddCSSClass("dim")
	right.Append(a.albumL)

	transport := gtk.NewBox(gtk.OrientationHorizontal, 6)
	right.Append(transport)
	prev := gtk.NewButtonWithLabel("⏮")
	a.playBtn = gtk.NewButtonWithLabel("▶")
	next := gtk.NewButtonWithLabel("⏭")
	prev.ConnectClicked(func() { a.playPrev() })
	a.playBtn.ConnectClicked(func() { _ = a.be.toggle() })
	next.ConnectClicked(func() { a.playNext() })
	transport.Append(prev)
	transport.Append(a.playBtn)
	transport.Append(next)

	seekRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	right.Append(seekRow)
	a.seek = gtk.NewScaleWithRange(gtk.OrientationHorizontal, 0, 100, 1)
	a.seek.SetDrawValue(false)
	a.seek.SetHExpand(true)
	a.seek.SetSensitive(false)
	a.seek.ConnectChangeValue(func(_ gtk.ScrollType, v float64) bool {
		a.scrubbed = nowMs()
		_ = a.be.seekAbs(int(v))
		return false
	})
	seekRow.Append(a.seek)
	a.posL = gtk.NewLabel("")
	a.posL.AddCSSClass("dim")
	seekRow.Append(a.posL)
}

func fmtTime(s int) string {
	if s < 0 {
		s = 0
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// poll mirrors TUI statusMsg: errors surface, natural ends advance,
// file changes reload art + accents + follow. Steady polls just
// move the progress widgets.
func (a *app) poll() {
	st, err := a.be.status()
	if err != nil {
		a.beErr = err.Error()
		return
	}
	a.beErr = ""
	if isNaturalEnd(a.prev, st) {
		prevFile := a.prev.File
		a.status = st
		a.prev = st
		a.advance(prevFile)
		a.refresh()
		return
	}
	changed := st.File != a.status.File
	a.prev = a.status
	a.status = st
	if changed {
		a.loadArt(st.File)
		a.followPlaying(st.File)
	}
	a.refresh()
}

// refresh pushes backend state into widgets. Skips slider moves
// shortly after a user scrub so polls don't fight the finger.
func (a *app) refresh() {
	st := a.status
	artist := st.Artist
	if artist == "" {
		artist = "—"
	}
	a.artistL.SetText(artist)
	a.titleL.SetText(st.Title)
	a.albumL.SetText(st.Album)
	a.noArt.SetVisible(st.File == "")
	if st.State == "paused" {
		a.playBtn.SetLabel("⏸")
	} else {
		a.playBtn.SetLabel("▶")
	}
	if st.Duration > 0 {
		a.seek.SetSensitive(true)
		a.seek.Adjustment().SetUpper(float64(st.Duration))
		if nowMs()-a.scrubbed > 1500 {
			a.seek.Adjustment().SetValue(float64(st.Position))
		}
		a.posL.SetText(fmtTime(st.Position) + " / " + fmtTime(st.Duration))
	} else {
		a.seek.SetSensitive(false)
		a.posL.SetText("")
	}
}

// loadArt decodes via the ported cache, paints a GdkTexture, and
// pushes the accent pair into the selection-only CSS override.
// Ceiling rule carried forward: per-track color touches the list
// selection state and nothing else — the base tokens never move.
func (a *app) loadArt(path string) {
	img, prim, sec, err := cachedArt(path)
	if err != nil || img == nil {
		a.art.SetPaintable(nil)
		a.noArt.SetVisible(true)
		a.applyAccent("", "")
		return
	}
	a.noArt.SetVisible(false)
	var buf bytes.Buffer
	if err := encodePNG(&buf, img); err == nil {
		if tex, err := gdk.NewTextureFromBytes(glib.NewBytes(buf.Bytes())); err == nil {
			a.art.SetPaintable(tex)
		}
	}
	a.applyAccent(prim, sec)
}

func (a *app) applyAccent(prim, sec string) {
	if prim == "" {
		prim = a.cfg.Theme.Accent
	}
	if sec == "" {
		sec = a.cfg.Theme.Accent2
	}
	// Selection surface only. Everything else stays on the base
	// brass/verdigris/ink tokens from style.css by construction:
	// this override names row:selected and nothing else.
	a.accent.LoadFromString(fmt.Sprintf(
		`.tracklist row:selected { background: %s; color: #161310; }`, prim))
}

// Transport through OUR order (queue-aware next), never cmus's
// single-file playlist — same bug class the TUI fixed.
func (a *app) playNext() {
	next, rest, ok := pickNext(a.queue, a.tracks, a.status.File, &a.shuf)
	if !ok {
		if a.status.File == "" && len(a.tracks) > 0 {
			_ = a.be.playFile(a.tracks[0].Path)
		}
		return
	}
	a.queue = rest
	_ = a.be.playFile(next)
}

func (a *app) playPrev() {
	prev, ok := prevTrackPath(a.tracks, a.status.File)
	if !ok {
		return
	}
	_ = a.be.playFile(prev)
}

func (a *app) playAt(i int) {
	if i < 0 || i >= len(a.view) {
		return
	}
	_ = a.be.playFile(a.view[i].Path)
}

func (a *app) advance(prevFile string) {
	next, rest, ok := pickNext(a.queue, a.tracks, prevFile, &a.shuf)
	if !ok {
		return
	}
	a.queue = rest
	_ = a.be.playFile(next)
}
