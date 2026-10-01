package main

// a-serenade-gtk — Phase 1: serenade's backend brain in a GTK4 body.
//
// Reused as-is from serenade: backend.go (cmus pty-supervisor),
// library.go (tag indexer), config.go (theme), art.go (accent
// extraction), player.go (transport decisions). Only this file and
// the view files are new. No libadwaita; plain GTK4 + style.css.

import (
	"fmt"
	"os"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type app struct {
	be     *backend
	cfg    Config
	tracks []Track
	view   []Track
	queue  []string
	shuf   shuffleState
	status Status
	prev   Status
	beErr  string

	win        *gtk.ApplicationWindow
	list       *gtk.ListView
	sel        *gtk.SingleSelection
	store      *gtk.StringList
	rows       []listRow
	listScroll *gtk.ScrolledWindow
	rowWidgets map[int]*gtk.Widget
	search     *gtk.SearchEntry
	shufBtn    *gtk.ToggleButton
	shufGlyph  *gtk.DrawingArea
	qWin       *gtk.Window
	qList      *gtk.ListBox
	qCursor    int
	widgetPos  map[*gtk.Widget]int // row widget to model pos
	lyrFile    string
	lyrLines   []lyricLine
	lyrSynced  bool
	lyrFlight  string // in-flight async fetch path; guards double fetch
	lyrBox     *gtk.Box
	lyrLabels  []*gtk.Label
	demoHold   bool    // demo state pins while backend is empty (screenshots)
	fGen       fadeGen // crossfade generation; rapid skips invalidate stale fades
	art        *gtk.Picture
	noArt      *gtk.Label
	artistL    *gtk.Label
	titleL     *gtk.Label
	albumL     *gtk.Label
	playBtn    *gtk.Button
	seek       *gtk.Scale
	posL       *gtk.Label
	durL       *gtk.Label
	bgPic      *gtk.Picture
	bgTex      *gdk.Texture
	tap        *vizTap
	viz        *gtk.DrawingArea
	vizW       int
	levels     []float64
	peaks      []float64
	vizActive  bool
	vizErr     string
	vizTick    glib.SourceHandle
	artPrim    string
	artSec     string
	accent     *gtk.CSSProvider
	scrubbed   int64 // ms timestamp of last user seek; polls defer to it
}

func main() {
	cfg := defaultConfig()
	cfg = loadConfig()

	be, err := ensureBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, "backend:", err)
		os.Exit(1)
	}
	tracks, err := indexLibrary()
	if err != nil {
		fmt.Fprintln(os.Stderr, "index:", err)
		os.Exit(1)
	}

	a := &app{be: be, cfg: cfg, tracks: tracks, view: tracks, tap: &vizTap{}}
	// ASER_APPID overrides the application ID so headless test
	// runs don't remote-activate (and steal focus from) a live
	// user instance on the same session bus. Display/test only.
	appID := os.Getenv("ASER_APPID")
	if appID == "" {
		appID = "org.serenade.gtk"
	}
	gtkApp := gtk.NewApplication(appID, gio.ApplicationFlagsNone)
	gtkApp.ConnectActivate(func() { a.activate(gtkApp) })
	os.Exit(gtkApp.Run(nil))
}

func (a *app) activate(app *gtk.Application) {
	display := gdk.DisplayGetDefault()
	base := gtk.NewCSSProvider()
	base.LoadFromString(baseCSS)
	gtk.StyleContextAddProviderForDisplay(display, base, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	a.accent = gtk.NewCSSProvider()
	gtk.StyleContextAddProviderForDisplay(display, a.accent, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION+1)

	a.win = gtk.NewApplicationWindow(app)
	a.win.SetTitle("Serenade")
	a.win.SetDefaultSize(1100, 700)

	// Phase 3 layering: blurred-art picture at the bottom, full UI
	// as the overlay child on top. The picture is never interactive
	// (no controllers), so all input falls through to the UI by
	// construction — no pass-through API needed. Scrim is baked
	// into the texture in Go (same adaptive math as the TUI),
	// never a second live layer.
	a.bgPic = gtk.NewPicture()
	a.bgPic.SetContentFit(gtk.ContentFitCover)
	a.bgPic.SetHExpand(true)
	a.bgPic.SetVExpand(true)
	overlay := gtk.NewOverlay()
	overlay.SetChild(a.bgPic)
	root := gtk.NewBox(gtk.OrientationVertical, 0)
	overlay.AddOverlay(root)
	a.win.SetChild(overlay)

	pane := gtk.NewPaned(gtk.OrientationHorizontal)
	pane.SetPosition(620)
	pane.SetVExpand(true)
	root.Append(pane)

	left := gtk.NewBox(gtk.OrientationVertical, 0)
	left.SetHExpand(true)
	left.SetVExpand(true)
	pane.SetStartChild(left)
	appName := gtk.NewLabel("Serenade")
	appName.SetXAlign(0)
	appName.AddCSSClass("app-header")
	appName.SetMarginStart(10)
	appName.SetMarginTop(8)
	left.Append(appName)
	libHead := gtk.NewLabel("Library")
	libHead.SetXAlign(0)
	libHead.AddCSSClass("library-heading")
	libHead.SetMarginStart(10)
	libHead.SetMarginTop(2)
	libHead.SetMarginBottom(4)
	left.Append(libHead)
	a.search = gtk.NewSearchEntry()
	a.search.SetPlaceholderText("Filter library")
	a.search.SetMarginStart(10)
	a.search.SetMarginEnd(10)
	a.search.SetMarginBottom(6)
	a.search.ConnectSearchChanged(func() { a.applySearch() })
	left.Append(a.search)
	scroll := gtk.NewScrolledWindow()
	scroll.SetHExpand(true)
	scroll.SetVExpand(true)
	left.Append(scroll)
	a.listScroll = scroll
	a.list = a.buildList()
	scroll.SetChild(a.list)

	right := gtk.NewBox(gtk.OrientationVertical, 8)
	right.SetHExpand(true)
	right.SetVExpand(true)
	right.SetMarginTop(12)
	right.SetMarginBottom(12)
	right.SetMarginStart(12)
	right.SetMarginEnd(12)
	// Explicit layout call (lyrics-vs-controls collision): the
	// CONTENT column (art, meta, lyrics) scrolls; the CONTROL
	// footer (transport, seek, queue, viz) is fixed and always
	// visible. Lyrics must never silently push the viz or the
	// queue row out of the window again.
	upper := gtk.NewBox(gtk.OrientationVertical, 8)
	upper.SetHExpand(true)
	scrollR := gtk.NewScrolledWindow()
	scrollR.SetHExpand(true)
	scrollR.SetVExpand(true)
	scrollR.SetChild(upper)
	right.Append(scrollR)
	footer := gtk.NewBox(gtk.OrientationVertical, 0)
	footer.SetHExpand(true)
	right.Append(footer)
	pane.SetEndChild(right)
	a.buildNowPlaying(upper, footer)

	a.poll()
	glib.TimeoutAdd(700, func() bool {
		a.poll()
		return true
	})
	// ASER_DEMO=file fabricates a playing state for screenshots in
	// headless sessions (no audio there): art, accents, follow and
	// progress render without touching playback.
	if demo := os.Getenv("ASER_DEMO"); demo != "" {
		a.demo(demo)
	}
	// ASER_DEMOSEARCH=query presets the filter box (screenshot
	// scaffolding for the search item; same pattern as ASER_DEMO).
	if sq := os.Getenv("ASER_DEMOSEARCH"); sq != "" && a.search != nil {
		a.search.SetText(sq)
	}
	// ASER_DEMOMENU=viewidx opens the track context menu for
	// that library row (screenshot scaffolding for the menu).
	if ms := os.Getenv("ASER_DEMOMENU"); ms != "" {
		var vidx int
		fmt.Sscanf(ms, "%d", &vidx)
		glib.TimeoutAdd(3000, func() bool {
			if vidx < 0 || vidx >= len(a.view) {
				return false
			}
			for r, lr := range a.rows {
				if lr.kind == rowTrack && lr.idx == vidx {
					if w, ok := a.rowWidgets[r]; ok && w != nil {
						a.openRowMenu(w, a.view[vidx].Path)
					}
					break
				}
			}
			return false
		})
	}
	// ASER_DEMOQUEUE=1 seeds three tracks and opens the queue
	// dialog (screenshot scaffolding for the queue item).
	if os.Getenv("ASER_DEMOQUEUE") != "" {
		for i := 0; i < 3 && i < len(a.tracks); i++ {
			a.queue = append(a.queue, a.tracks[i].Path)
		}
		a.showQueueDialog()
	}
	// ASER_DEMOSEQ=f1,f2,... cycles demo tracks every 350ms to
	// exercise fade overlap headlessly (the rapid-fire edge).
	// Display path only; the guard itself is unit-tested.
	if seq := os.Getenv("ASER_DEMOSEQ"); seq != "" {
		files := strings.Split(seq, ",")
		i := 0
		glib.TimeoutAdd(350, func() bool {
			i = (i + 1) % len(files)
			a.demo(files[i])
			return true
		})
	}
	a.win.ConnectCloseRequest(func() bool {
		a.stopViz()
		return false
	})
	a.attachKeys()
	a.win.Present()
}

// applySearch live-filters the library through the same fuzzy
// engine as the TUI, then rebuilds rows/model in place (splice
// keeps the selection model alive). Selection lands back on the
// playing row when still present, else the top.
func (a *app) applySearch() {
	if a.search == nil || a.store == nil {
		return
	}
	a.view = filterTracks(a.tracks, a.search.Text())
	rows, labels := listRows(a.view)
	a.rows = rows
	a.rowWidgets = map[int]*gtk.Widget{}
	a.widgetPos = map[*gtk.Widget]int{}
	a.store.Splice(0, a.store.NItems(), labels)
	a.followPlaying(a.status.File)
	if a.status.File == "" {
		a.sel.SetSelected(0)
	}
}

// demo loads art/accents/selection for file and fakes a playing
// status line. Screenshot scaffolding; never affects playback.
func (a *app) demo(file string) {
	var tr Track
	for _, t := range a.tracks {
		if t.Path == file {
			tr = t
		}
	}
	a.status = Status{State: "playing", File: file, Artist: tr.Artist,
		Title: tr.Title, Album: tr.Album, Duration: 184, Position: 61}
	a.demoHold = true
	a.loadArt(file)
	a.resolveLyrics(file)
	a.followPlaying(file)
	// ASER_DEMOLEVELS=1 seeds a synthetic spectrum so screenshots
	// can show the Cairo bars where headless sessions have no
	// audio. Display path only; the FFT engine is unit-tested.
	if os.Getenv("ASER_DEMOLEVELS") != "" {
		n := 32
		a.vizActive = true
		a.levels = make([]float64, n)
		a.peaks = make([]float64, n)
		for i := range a.levels {
			a.levels[i] = 0.25 + 0.65*float64((i*37+11)%10)/10.0
			a.peaks[i] = a.levels[i] + 0.08
		}
		// Re-queue after mapping: the first draw can land on a
		// zero-width allocation (paints nothing) with no tick to
		// follow it in demo mode.
		glib.TimeoutAdd(1500, func() bool {
			if a.viz != nil {
				a.viz.QueueDraw()
			}
			return false
		})
	}
	a.refresh()
}

// nowMs is wall clock for the scrub guard.
func nowMs() int64 {
	return glib.GetMonotonicTime() / 1000
}

func (a *app) playingPath() string { return a.status.File }
