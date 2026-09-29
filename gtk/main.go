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

	win       *gtk.ApplicationWindow
	list      *gtk.ListView
	sel       *gtk.SingleSelection
	store     *gtk.StringList
	rows      []listRow
	art       *gtk.Picture
	noArt     *gtk.Label
	artistL   *gtk.Label
	titleL    *gtk.Label
	albumL    *gtk.Label
	playBtn   *gtk.Button
	seek      *gtk.Scale
	posL      *gtk.Label
	durL      *gtk.Label
	tap       *vizTap
	viz       *gtk.DrawingArea
	vizW      int
	levels    []float64
	peaks     []float64
	vizActive bool
	vizErr    string
	vizTick   glib.SourceHandle
	artPrim   string
	artSec    string
	accent    *gtk.CSSProvider
	scrubbed  int64 // ms timestamp of last user seek; polls defer to it
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
	gtkApp := gtk.NewApplication("org.serenade.gtk", gio.ApplicationFlagsNone)
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

	root := gtk.NewBox(gtk.OrientationVertical, 0)
	a.win.SetChild(root)

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
	scroll := gtk.NewScrolledWindow()
	scroll.SetHExpand(true)
	scroll.SetVExpand(true)
	left.Append(scroll)
	a.list = a.buildList()
	scroll.SetChild(a.list)

	right := gtk.NewBox(gtk.OrientationVertical, 8)
	right.SetHExpand(true)
	right.SetMarginTop(12)
	right.SetMarginBottom(12)
	right.SetMarginStart(12)
	right.SetMarginEnd(12)
	pane.SetEndChild(right)
	a.buildNowPlaying(right)

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
	a.win.ConnectCloseRequest(func() bool {
		a.stopViz()
		return false
	})
	a.win.Present()
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
	a.loadArt(file)
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
	}
	a.refresh()
}

// nowMs is wall clock for the scrub guard.
func nowMs() int64 {
	return glib.GetMonotonicTime() / 1000
}

func (a *app) playingPath() string { return a.status.File }
