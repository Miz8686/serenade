package main

// nowplaying.go — right panel: art (GdkTexture), type hierarchy,
// transport, seek. Poll loop mirrors the TUI statusMsg choke points:
// file change → art + accents + follow; natural end → advance.

import (
	"bytes"
	"fmt"
	"image"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

func (a *app) buildNowPlaying(right *gtk.Box) {
	a.art = gtk.NewPicture()
	a.art.SetSizeRequest(320, 320)
	a.art.SetMarginBottom(12)
	a.art.AddCSSClass("cover")
	right.Append(a.art)

	a.noArt = gtk.NewLabel("no cover art")
	a.noArt.AddCSSClass("dim")
	a.noArt.SetVisible(false)
	right.Append(a.noArt)

	a.artistL = gtk.NewLabel("")
	a.artistL.SetXAlign(0)
	a.artistL.AddCSSClass("artist-subtitle")
	right.Append(a.artistL)

	a.titleL = gtk.NewLabel("")
	a.titleL.SetXAlign(0)
	a.titleL.SetEllipsize(pango.EllipsizeEnd)
	a.titleL.AddCSSClass("track-title-large")
	right.Append(a.titleL)

	a.albumL = gtk.NewLabel("")
	a.albumL.SetXAlign(0)
	a.albumL.SetEllipsize(pango.EllipsizeEnd)
	a.albumL.AddCSSClass("dim")
	right.Append(a.albumL)

	transport := gtk.NewBox(gtk.OrientationHorizontal, 6)
	transport.SetHAlign(gtk.AlignCenter)
	right.Append(transport)
	prev := gtk.NewButtonFromIconName("media-skip-backward")
	prev.SetTooltipText("Previous track")
	prev.AddCSSClass("transport-btn")
	a.playBtn = gtk.NewButtonFromIconName("media-playback-start")
	a.playBtn.SetTooltipText("Play / pause")
	a.playBtn.AddCSSClass("play-primary")
	next := gtk.NewButtonFromIconName("media-skip-forward")
	next.SetTooltipText("Next track")
	next.AddCSSClass("transport-btn")
	prev.ConnectClicked(func() { a.playPrev() })
	a.playBtn.ConnectClicked(func() { _ = a.be.toggle() })
	next.ConnectClicked(func() { a.playNext() })
	transport.Append(prev)
	transport.Append(a.playBtn)
	transport.Append(next)

	seekRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	seekRow.AddCSSClass("seek-row")
	seekRow.SetMarginTop(8)
	seekRow.SetMarginBottom(4)
	right.Append(seekRow)
	a.posL = gtk.NewLabel("")
	a.posL.AddCSSClass("dim")
	seekRow.Append(a.posL)
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
	a.durL = gtk.NewLabel("")
	// Phase 2 strip: below transport+seek, compact by design.
	a.buildViz(right)
	a.durL.AddCSSClass("dim")
	seekRow.Append(a.durL)
}

// accentCSS builds the runtime override: selection surface,
// transport hover/press, and seek fill carry the per-track prim.
// Pure (and unit-tested) so a typo here can't silently drop a rule.
func accentCSS(prim string) string {
	return fmt.Sprintf(`
.tracklist row:selected { background: %s; color: %s; }
.transport-btn:hover, .play-primary:hover { background-color: alpha(%s, 0.22); }
.transport-btn:active, .play-primary:active { background-color: alpha(%s, 0.38); }
.seek-row scale highlight { background-color: %s; }
`, prim, pickText(prim), prim, prim, prim)
}

// pickText returns paper or ink, whichever contrasts with the
// given accent background. Same job as the TUI's selected-row fg.
func pickText(hex string) string {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	lum := 0.2126*float64(r)/255 + 0.7152*float64(g)/255 + 0.0722*float64(b)/255
	if lum > 0.25 {
		return "#161310"
	}
	return "#EDE0C8"
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
	oldFile := a.status.File
	a.prev = a.status
	a.status = st
	if changed {
		a.loadArt(st.File)
		a.followPlaying(st.File)
		a.rebindRow(oldFile)
		a.rebindRow(st.File)
		a.restartTap()
	}
	a.ensureViz()
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
		a.playBtn.SetIconName("media-playback-pause")
	} else {
		a.playBtn.SetIconName("media-playback-start")
	}
	if st.Duration > 0 {
		a.seek.SetSensitive(true)
		a.seek.Adjustment().SetUpper(float64(st.Duration))
		if nowMs()-a.scrubbed > 1500 {
			a.seek.Adjustment().SetValue(float64(st.Position))
		}
		a.posL.SetText(fmtTime(st.Position))
		a.durL.SetText(fmtTime(st.Duration))
	} else {
		a.seek.SetSensitive(false)
		a.posL.SetText("")
		a.durL.SetText("")
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
		a.artPrim, a.artSec = "", ""
		a.applyAccent("", "")
		a.setBackdrop(nil)
		return
	}
	a.noArt.SetVisible(false)
	a.artPrim, a.artSec = prim, sec
	var buf bytes.Buffer
	if err := encodePNG(&buf, img); err == nil {
		if tex, err := gdk.NewTextureFromBytes(glib.NewBytes(buf.Bytes())); err == nil {
			a.art.SetPaintable(tex)
		}
	}
	a.applyAccent(prim, sec)
	a.setBackdrop(img)
}

// setBackdrop builds the Phase 3 atmosphere exactly once per track:
// downscale (the blur — GPU upscaling smooths it for free), adaptive
// scrim baked in (same floors as the TUI contrast suite), one texture
// held until the next track. Replacing bgTex drops the old reference;
// gotk4 finalizers reap the GL side. Nil clears back to ink.
func (a *app) setBackdrop(img image.Image) {
	if img == nil {
		a.bgTex = nil
		a.bgPic.SetPaintable(nil)
		return
	}
	bg := scrimAdaptive(smallBlur(toRGBA(img)), a.cfg.Theme.Bg)
	var buf bytes.Buffer
	if err := encodePNG(&buf, bg); err != nil {
		return
	}
	if tex, err := gdk.NewTextureFromBytes(glib.NewBytes(buf.Bytes())); err == nil {
		a.bgTex = tex
		a.bgPic.SetPaintable(tex)
	}
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
	// Dynamic accent: selection surface, transport hover/press,
	// and seek fill — and nothing else. Base tokens in style.css
	// never move, so the identity holds regardless of the cover.
	// Selection text picks whichever of paper/ink contrasts with
	// the accent (brass needs dark text, deep reds need paper).
	a.accent.LoadFromString(accentCSS(prim))
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
