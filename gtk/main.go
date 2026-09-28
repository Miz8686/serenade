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

	win      *gtk.ApplicationWindow
	list     *gtk.ListView
	sel      *gtk.SingleSelection
	store    *gtk.StringList
	rows     []listRow
	art      *gtk.Picture
	noArt    *gtk.Label
	artistL  *gtk.Label
	titleL   *gtk.Label
	albumL   *gtk.Label
	playBtn  *gtk.Button
	seek     *gtk.Scale
	posL     *gtk.Label
	accent   *gtk.CSSProvider
	scrubbed int64 // ms timestamp of last user seek; polls defer to it
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

	a := &app{be: be, cfg: cfg, tracks: tracks, view: tracks}
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

	left := gtk.NewScrolledWindow()
	left.SetHExpand(true)
	left.SetVExpand(true)
	pane.SetStartChild(left)
	a.list = a.buildList()
	left.SetChild(a.list)

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
	a.refresh()
}

// nowMs is wall clock for the scrub guard.
func nowMs() int64 {
	return glib.GetMonotonicTime() / 1000
}
