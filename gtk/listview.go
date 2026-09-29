package main

// listview.go — library ListView with artist headings baked into the
// model as non-selectable rows (same liner-notes structure as the
// TUI: heading kinds + track kinds, one factory branching on kind).
//
// Why not GtkListView's header factory: without a section model it
// only ever binds position 0 (verified live — a single heading up
// top, nothing after). Kinds-in-model gives full control with real
// widgets and real Pango hierarchy either way.

import (
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/diamondburned/gotk4/pkg/core/glib"
	glib2 "github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

type rowKind int

const (
	rowTrack rowKind = iota
	rowArtist
)

type listRow struct {
	kind rowKind
	idx  int // view index for tracks
}

// listRows builds the liner-notes rows: artist heading at each
// group start (source casing kept — hierarchy comes from weight,
// not shouting), then one two-line row per track.
func listRows(view []Track) ([]listRow, []string) {
	var rows []listRow
	var labels []string
	prev := ""
	first := true
	for i, t := range view {
		ak := strings.ToLower(t.Artist)
		if t.Artist != "" && (first || ak != prev) {
			rows = append(rows, listRow{rowArtist, i})
			labels = append(labels, t.Artist)
			prev, first = ak, false
		} else if first {
			first, prev = false, ak
		}
		rows = append(rows, listRow{rowTrack, i})
		labels = append(labels, t.Title)
	}
	return rows, labels
}

func (a *app) buildList() *gtk.ListView {
	rows, labels := listRows(a.view)
	a.rows = rows
	a.rowWidgets = map[int]*gtk.Widget{}
	a.store = gtk.NewStringList(labels)
	a.sel = gtk.NewSingleSelection(a.store)
	a.sel.SetCanUnselect(false)

	factory := gtk.NewSignalListItemFactory()
	factory.ConnectSetup(func(o *glib.Object) {
		box := gtk.NewBox(gtk.OrientationVertical, 0)
		box.AddCSSClass("library-list-item")
		title := gtk.NewLabel("")
		title.SetXAlign(0)
		title.SetEllipsize(pango.EllipsizeEnd)
		title.AddCSSClass("track-title")
		meta := gtk.NewLabel("")
		meta.SetXAlign(0)
		meta.SetEllipsize(pango.EllipsizeEnd)
		meta.AddCSSClass("track-meta")
		box.Append(title)
		box.Append(meta)
		o.Cast().(*gtk.ListItem).SetChild(box)
	})
	factory.ConnectBind(func(o *glib.Object) {
		it := o.Cast().(*gtk.ListItem)
		pos := int(it.Position())
		if pos < 0 || pos >= len(a.rows) {
			return
		}
		r := a.rows[pos]
		box := it.Child().(*gtk.Box)
		// Track the live widget per model row so centerRow can
		// measure the playing row's pixel position. Updated on
		// every bind; entries pointing at this same (recycled)
		// widget are dropped first, so a stale row never keeps
		// a widget that now displays another position. Cleared
		// on model rebuilds (buildList, applySearch).
		if a.rowWidgets == nil {
			a.rowWidgets = map[int]*gtk.Widget{}
		}
		w := &box.Widget
		for p, old := range a.rowWidgets {
			if old == w {
				delete(a.rowWidgets, p)
			}
		}
		a.rowWidgets[pos] = w
		title := box.FirstChild().(*gtk.Label)
		meta := title.NextSibling().(*gtk.Label)
		if r.kind == rowArtist {
			// Headings reuse the two-line row: name up top in
			// heading style, meta line cleared so recycled rows
			// never leak stale album text. Even rhythm kept.
			title.SetText(a.view[r.idx].Artist)
			title.AddCSSClass("artist-heading")
			title.RemoveCSSClass("track-title")
			meta.SetText("")
			it.SetSelectable(false)
			it.SetActivatable(false)
			return
		}
		t := a.view[r.idx]
		title.RemoveCSSClass("artist-heading")
		title.AddCSSClass("track-title")
		meta.SetText(albumLabel(a.view, a.rows, pos, t))
		// Playing state is explicit, never color-alone: note
		// marker plus the selection that followPlaying drives.
		name := t.Title
		if t.Path == a.playingPath() && t.Path != "" {
			name = "♪ " + name
		}
		title.SetText(name)
		it.SetSelectable(true)
		it.SetActivatable(true)
	})

	lv := gtk.NewListView(a.sel, &factory.ListItemFactory)
	lv.SetShowSeparators(false)
	lv.SetSingleClickActivate(false)
	lv.ConnectActivate(func(pos uint) {
		if int(pos) < len(a.rows) && a.rows[pos].kind == rowTrack {
			a.playAt(a.rows[pos].idx)
		}
	})
	lv.AddCSSClass("tracklist")
	return lv
}

// albumLabel resolves the second-line metadata for a track row:
// the album name shown once per contiguous (artist, album) run and
// suppressed for repeats, and suppressed entirely when it merely
// echoes the title (the singles case). Pure for testing.
func albumLabel(view []Track, rows []listRow, pos int, t Track) string {
	al := strings.TrimSpace(t.Album)
	if al == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(t.Title), al) {
		return ""
	}
	for p := pos - 1; p >= 0; p-- {
		if rows[p].kind != rowTrack {
			continue
		}
		pt := view[rows[p].idx]
		if strings.ToLower(pt.Artist) != strings.ToLower(t.Artist) {
			break
		}
		if pt.Album == t.Album {
			return ""
		}
		break
	}
	return t.Album
}

// filterTracks fuzzy-filters tracks on "Artist – Title" labels,
// same engine and behavior as the TUI's `/`. Empty query restores
// the full library. Pure for testing.
func filterTracks(tracks []Track, q string) []Track {
	if strings.TrimSpace(q) == "" {
		return tracks
	}
	labels := make([]string, len(tracks))
	for i, t := range tracks {
		if t.Artist != "" {
			labels[i] = t.Artist + " – " + t.Title
		} else {
			labels[i] = t.Title
		}
	}
	matches := fuzzy.Find(q, labels)
	out := make([]Track, 0, len(matches))
	for _, mt := range matches {
		out = append(out, tracks[mt.Index])
	}
	return out
}

// rowForPath is the ONE mapping from a track file to its row in
// the composite model (headings included). Every trigger path —
// manual selection, auto-advance, next, prev — funnels through
// it via followPlaying (and rebindRow for marker refresh), so
// there is exactly one index space and no per-trigger arithmetic
// left to drift. Returns -1 when the file isn't in the view
// (filtered out, empty). Pure for testing.
func rowForPath(view []Track, rows []listRow, path string) int {
	if path == "" {
		return -1
	}
	for i, t := range view {
		if t.Path != path {
			continue
		}
		for r, lr := range rows {
			if lr.kind == rowTrack && lr.idx == i {
				return r
			}
		}
		return -1
	}
	return -1
}

// rebindRow forces ListView to re-run bind for one track row.
// The ♪ marker reads live playback state (not model data), so rows
// must rebind on track change or the marker goes stale.
func (a *app) rebindRow(path string) {
	if r := rowForPath(a.view, a.rows, path); r >= 0 {
		a.store.Splice(uint(r), 1, []string{a.store.String(uint(r))})
	}
}

// followPlaying moves selection to the playing track's row and
// centers it in the viewport — same deterministic landing the TUI
// gets from offset = i - vis/2, for every trigger alike. GTK's
// bare ScrollTo only reveals with minimal travel (playing row
// docks wherever the scroll direction leaves it: top edge coming
// from above, bottom edge from below), which is exactly the
// "opposite wrong ends" symptom. So: reveal first (correct even
// if centering misses), then center on idle after layout.
func (a *app) followPlaying(path string) {
	r := rowForPath(a.view, a.rows, path)
	if r < 0 {
		return
	}
	a.sel.SetSelected(uint(r))
	a.list.ScrollTo(uint(r), gtk.ListScrollNone, nil)
	a.centerRow(path, r)
}

// centerRow scrolls the list's viewport so model row r sits
// mid-window. Runs on idle (post-layout, so the row widget freshly
// bound by the reveal above is measured, never a recycled stale
// one) and applies only if path is still current — a rapid skip in
// between aborts the stale centering instead of yanking the view.
// Up to three idle passes: the first may run before the reveal's
// layout rebinds the target row.
func (a *app) centerRow(path string, r int) {
	if a.listScroll == nil {
		return
	}
	tries := 0
	var attempt func() bool
	attempt = func() bool {
		if a.status.File != path || a.listScroll == nil {
			return false
		}
		w, ok := a.rowWidgets[r]
		if !ok || w == nil {
			tries++
			if tries < 3 {
				glib2.IdleAdd(attempt)
			}
			return false
		}
		_, wy, ok := w.TranslateCoordinates(&a.list.ListBase.Widget, 0, 0)
		if !ok {
			return false
		}
		adj := a.listScroll.VAdjustment()
		page := adj.PageSize()
		if page <= 0 {
			return false
		}
		// wy is viewport-relative (GtkListView positions children
		// in view space — verified live: the same widget measured
		// 612 while the adjustment sat at 7482), so the
		// content-space row center is v0 + wy + h/2.
		target := adj.Value() + wy + float64(w.AllocatedHeight())/2 - page/2
		target = min(max(target, adj.Lower()), max(adj.Lower(), adj.Upper()-page))
		adj.SetValue(target)
		return false
	}
	glib2.IdleAdd(attempt)
}
