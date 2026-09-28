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

	"github.com/diamondburned/gotk4/pkg/core/glib"
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
	a.store = gtk.NewStringList(labels)
	a.sel = gtk.NewSingleSelection(a.store)
	a.sel.SetCanUnselect(false)

	factory := gtk.NewSignalListItemFactory()
	factory.ConnectSetup(func(o *glib.Object) {
		box := gtk.NewBox(gtk.OrientationVertical, 0)
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
		meta.SetText(t.Album)
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

// rebindRow forces ListView to re-run bind for one track row.
// The ♪ marker reads live playback state (not model data), so rows
// must rebind on track change or the marker goes stale.
func (a *app) rebindRow(path string) {
	if path == "" {
		return
	}
	for i, t := range a.view {
		if t.Path != path {
			continue
		}
		for r, lr := range a.rows {
			if lr.kind == rowTrack && lr.idx == i {
				a.store.Splice(uint(r), 1, []string{a.store.String(uint(r))})
				return
			}
		}
		return
	}
}

// followPlaying moves selection to the playing track's row.
func (a *app) followPlaying(path string) {
	if path == "" {
		return
	}
	for i, t := range a.view {
		if t.Path == path {
			for r, lr := range a.rows {
				if lr.kind == rowTrack && lr.idx == i {
					a.sel.SetSelected(uint(r))
					a.list.ScrollTo(uint(r), gtk.ListScrollNone, nil)
					return
				}
			}
			return
		}
	}
}
