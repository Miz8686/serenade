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
// group start (uppercase display, original index kept for tracks).
func listRows(view []Track) ([]listRow, []string) {
	var rows []listRow
	var labels []string
	prev := ""
	first := true
	for i, t := range view {
		ak := strings.ToLower(t.Artist)
		if t.Artist != "" && (first || ak != prev) {
			rows = append(rows, listRow{rowArtist, i})
			labels = append(labels, strings.ToUpper(t.Artist))
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
		lbl := gtk.NewLabel("")
		lbl.SetXAlign(0)
		lbl.SetEllipsize(pango.EllipsizeEnd)
		o.Cast().(*gtk.ListItem).SetChild(lbl)
	})
	factory.ConnectBind(func(o *glib.Object) {
		it := o.Cast().(*gtk.ListItem)
		pos := int(it.Position())
		if pos < 0 || pos >= len(a.rows) {
			return
		}
		r := a.rows[pos]
		lbl := it.Child().(*gtk.Label)
		if r.kind == rowArtist {
			lbl.SetText(strings.ToUpper(a.view[r.idx].Artist))
			lbl.AddCSSClass("artist-heading")
			lbl.RemoveCSSClass("track-row")
			it.SetSelectable(false)
			it.SetActivatable(false)
		} else {
			lbl.SetText(a.view[r.idx].Title)
			lbl.AddCSSClass("track-row")
			lbl.RemoveCSSClass("artist-heading")
			it.SetSelectable(true)
			it.SetActivatable(true)
		}
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
	// Initial selection: first track row, never a heading.
	for r, lr := range rows {
		if lr.kind == rowTrack {
			a.sel.SetSelected(uint(r))
			break
		}
	}
	return lv
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
