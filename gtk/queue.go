package main

// queue.go — queue + shuffle UI on the TUI's semantics.
//
// Underlying logic is the TUI's verbatim: the queue is the live
// slice pickNext() drains (no copy, no parallel representation),
// queueMove swaps in place with the cursor following the moved
// item, queueRemove drops and clamps. Shuffle stays a shuffle-bag
// (freshBag): the "Shuffle play" BUTTON starts a fresh shuffled
// session right now, while the toggle key/button flips the mode
// only and never interrupts the current track.
//
// Surface is new: a quiet button row under seek (toggle + shuffle
// play + enqueue + queue view) and a modal queue dialog (ListBox
// with Up/Down/Remove). No libadwaita; plain GTK4.

import (
	"path/filepath"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// queueMove swaps item i with its neighbor in dir (-1 up, +1 down);
// the cursor follows the moved item. Boundary moves are no-ops.
// Ported verbatim from the TUI — same in-place slice pickNext()
// drains.
func queueMove(q []string, i, dir int) ([]string, int) {
	j := i + dir
	if i < 0 || i >= len(q) || j < 0 || j >= len(q) {
		return q, min(max(i, 0), max(0, len(q)-1))
	}
	q[i], q[j] = q[j], q[i]
	return q, j
}

// queueRemove drops item i, clamping the cursor into range.
// Ported verbatim from the TUI.
func queueRemove(q []string, i int) ([]string, int) {
	if len(q) == 0 {
		return q, 0
	}
	i = min(max(i, 0), len(q)-1)
	q = append(q[:i], q[i+1:]...)
	return q, min(i, max(0, len(q)-1))
}

// trackByPath resolves a queue path to its library track for
// display. Fallback is the file basename, never empty.
func trackByPath(tracks []Track, path string) Track {
	for _, t := range tracks {
		if t.Path == path {
			return t
		}
	}
	return Track{Path: path, Title: filepath.Base(path)}
}

// queueLabel is the one-line display for a queue entry. Pure for
// testing.
func queueLabel(t Track) string {
	if t.Artist != "" {
		return t.Artist + " – " + t.Title
	}
	if t.Title != "" {
		return t.Title
	}
	return filepath.Base(t.Path)
}

// reshuffleAndPlay is the shuffle BUTTON behavior (distinct from
// the toggle): a fresh bag avoiding the current track, drained
// immediately so playback starts now. Ported from the TUI.
func (a *app) reshuffleAndPlay() {
	if len(a.tracks) == 0 {
		return
	}
	curIdx := -1
	for i, t := range a.tracks {
		if t.Path == a.status.File {
			curIdx = i
		}
	}
	a.shuf.on = true
	a.shuf.order = freshBag(len(a.tracks), curIdx)
	a.shuf.pos = 0
	next, rest, ok := pickNext(a.queue, a.tracks, "", &a.shuf)
	a.queue = rest
	if !ok {
		return
	}
	_ = a.be.playFile(next)
	a.syncShuffleUI()
}

// toggleShuffle flips shuffle mode only: turning on arms the bag
// for future advances, turning off never interrupts the current
// track. Unlike the TUI (which autostarts when idle), the GTK
// toggle never starts playback itself — starting now is the
// "Shuffle play" button's job (spec: button starts, toggle is
// mode only).
func (a *app) toggleShuffle() {
	if a.shuf.on {
		a.shuf.on = false
		a.shuf.order = nil
		a.shuf.pos = 0
		a.syncShuffleUI()
		return
	}
	if len(a.tracks) == 0 {
		return
	}
	a.shuf.on = true
	a.shuf.order = freshBag(len(a.tracks), -1)
	a.shuf.pos = 0
	a.syncShuffleUI()
}

// enqueueSelected appends the selected library row to the queue
// (the TUI's "a" on the cursor).
func (a *app) enqueueSelected() {
	if a.sel == nil {
		return
	}
	pos := int(a.sel.Selected())
	if pos < 0 || pos >= len(a.rows) || a.rows[pos].kind != rowTrack {
		return
	}
	a.queue = append(a.queue, a.view[a.rows[pos].idx].Path)
	a.refreshQueueDialog()
}

// buildQueueBar adds the quiet secondary row under seek: shuffle
// toggle, shuffle-play, enqueue, queue view. Serenade-drawn Cairo
// glyphs (icons.go) with text in tooltips — same frame, sizing and
// spacing as the transport row above, own marks throughout.
func (a *app) buildQueueBar(parent *gtk.Box) {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bar.SetHAlign(gtk.AlignCenter)
	bar.SetMarginTop(4)
	parent.Append(bar)

	shufB, shufG := glyphToggleButton("Shuffle mode (s)", a.drawShuffle)
	a.shufBtn, a.shufGlyph = shufB, shufG
	shufB.ConnectToggled(func() {
		want := shufB.Active()
		if want != a.shuf.on {
			a.toggleShuffle()
		}
		// toggleShuffle syncs back; a programmatic sync
		// re-emitting toggled would loop, so sync only on
		// mismatch (handled inside toggleShuffle).
		shufG.QueueDraw()
	})
	bar.Append(shufB)

	shufPlay, _ := glyphButton("Shuffle play \u2014 fresh session, starting now", a.drawReshuffle)
	shufPlay.ConnectClicked(func() { a.reshuffleAndPlay() })
	bar.Append(shufPlay)

	enq, _ := glyphButton("Add to queue (a)", a.drawEnqueue)
	enq.ConnectClicked(func() { a.enqueueSelected() })
	bar.Append(enq)

	qview, _ := glyphButton("Queue (A)", a.drawQueueList)
	qview.ConnectClicked(func() { a.showQueueDialog() })
	bar.Append(qview)
}

// syncShuffleUI reflects the mode bit on the toggle without
// re-entering the toggled handler loop.
func (a *app) syncShuffleUI() {
	if a.shufBtn == nil {
		return
	}
	if a.shufBtn.Active() != a.shuf.on {
		a.shufBtn.SetActive(a.shuf.on)
	}
	if a.shufGlyph != nil {
		a.shufGlyph.QueueDraw()
	}
}

// showQueueDialog opens (or focuses) the modal queue panel: live
// ListBox over a.queue with Up/Down/Remove acting in place. Panel
// chrome (surface, brass-kissed border) and icon buttons match the
// main window's design system — same .transport-btn treatment and
// Serenade-drawn glyphs as the secondary row.
func (a *app) showQueueDialog() {
	if a.qWin != nil {
		a.qWin.Present()
		return
	}
	w := gtk.NewWindow()
	w.SetTitle("Queue")
	w.SetDefaultSize(420, 380)
	w.SetTransientFor(&a.win.Window)
	w.SetModal(true)
	w.ConnectCloseRequest(func() bool {
		a.qWin = nil
		a.qList = nil
		return false
	})

	box := gtk.NewBox(gtk.OrientationVertical, 6)
	box.AddCSSClass("queue-panel")
	box.SetMarginTop(10)
	box.SetMarginBottom(10)
	box.SetMarginStart(10)
	box.SetMarginEnd(10)
	w.SetChild(box)

	head := gtk.NewLabel("Queue")
	head.SetXAlign(0)
	head.AddCSSClass("queue-heading")
	box.Append(head)

	scroll := gtk.NewScrolledWindow()
	scroll.SetVExpand(true)
	scroll.SetHExpand(true)
	box.Append(scroll)
	a.qList = gtk.NewListBox()
	a.qList.AddCSSClass("tracklist")
	scroll.SetChild(a.qList)

	btns := gtk.NewBox(gtk.OrientationHorizontal, 6)
	btns.SetHAlign(gtk.AlignCenter)
	box.Append(btns)
	up, _ := glyphButton("Move up", a.drawArrowUp)
	up.ConnectClicked(func() {
		a.queue, a.qCursor = queueMove(a.queue, a.qCursor, -1)
		a.refreshQueueDialog()
	})
	btns.Append(up)

	down, _ := glyphButton("Move down", a.drawArrowDown)
	down.ConnectClicked(func() {
		a.queue, a.qCursor = queueMove(a.queue, a.qCursor, +1)
		a.refreshQueueDialog()
	})
	btns.Append(down)

	rm, _ := glyphButton("Remove from queue", a.drawMinus)
	rm.ConnectClicked(func() {
		a.queue, a.qCursor = queueRemove(a.queue, a.qCursor)
		a.refreshQueueDialog()
	})
	btns.Append(rm)

	closeB, _ := glyphButton("Close", a.drawCloseX)
	closeB.ConnectClicked(func() { w.Close() })
	btns.Append(closeB)
	a.qList.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row != nil {
			a.qCursor = row.Index()
		}
	})

	a.qWin = w
	a.qCursor = 0
	a.refreshQueueDialog()
	w.Present()
}

// queueMeta is the second line under a queue entry: artist and
// album when known, falling back to the file basename. Pure.
func queueMeta(t Track) string {
	if t.Artist != "" && t.Album != "" {
		return t.Artist + " – " + t.Album
	}
	if t.Album != "" {
		return t.Album
	}
	if t.Artist != "" {
		return t.Artist
	}
	return filepath.Base(t.Path)
}

// refreshQueueDialog rebuilds the ListBox from the live queue.
// Rows get the library two-line treatment (title + muted meta).
// No-op when the dialog is closed.
func (a *app) refreshQueueDialog() {
	if a.qList == nil {
		return
	}
	a.qList.RemoveAll()
	for _, p := range a.queue {
		t := trackByPath(a.tracks, p)
		title := gtk.NewLabel(queueLabel(t))
		title.SetXAlign(0)
		title.AddCSSClass("track-title")
		meta := gtk.NewLabel(queueMeta(t))
		meta.SetXAlign(0)
		meta.AddCSSClass("track-meta")
		cell := gtk.NewBox(gtk.OrientationVertical, 0)
		cell.Append(title)
		cell.Append(meta)
		row := gtk.NewListBoxRow()
		row.SetChild(cell)
		a.qList.Append(row)
	}
	if len(a.queue) == 0 {
		empty := gtk.NewLabel("Queue is empty — press a or right-click a track to add it.")
		empty.AddCSSClass("dim")
		row := gtk.NewListBoxRow()
		row.SetSelectable(false)
		row.SetChild(empty)
		a.qList.Append(row)
		return
	}
	if a.qCursor < 0 || a.qCursor >= len(a.queue) {
		a.qCursor = min(max(a.qCursor, 0), len(a.queue)-1)
	}
	if r := a.qList.RowAtIndex(a.qCursor); r != nil {
		a.qList.SelectRow(r)
	}
}
