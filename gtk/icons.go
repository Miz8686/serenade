package main

// icons.go — Serenade's own secondary-control glyphs, drawn in
// Cairo. Not another icon theme's set: shuffle as two crossing
// paths, reshuffle as a broken ring, enqueue as plus-over-lines,
// queue as lines-with-marker. Simple geometric marks in the house
// voice (paper, brass for the armed shuffle state), same 22px box
// and .transport-btn frame as the row above.

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// glyphSize is the fixed drawing box: matches the visual weight
// of the symbolic transport glyphs above.
const glyphSize = 22

// glyphColors resolves the stroke color: brass when armed (shuffle
// on), paper otherwise. Fixed brand tokens — never per-track.
func glyphColors(armed bool) (float64, float64, float64) {
	if armed {
		return hexRGB("#D9A44C")
	}
	return hexRGB("#EDE0C8")
}

// glyphButton builds a flat icon button around a Cairo drawing
// area. draw receives the live app state each repaint (same
// pattern as the viz strip), so state changes only need a
// QueueDraw. Returns both for redraw wiring.
// glyphToggleButton is the toggle variant (shuffle mode keeps
// a real GtkToggleButton so keyboard/AT state stays honest).
func glyphToggleButton(tooltip string, draw func(cr *cairo.Context, w, h int)) (*gtk.ToggleButton, *gtk.DrawingArea) {
	da := gtk.NewDrawingArea()
	da.SetContentWidth(glyphSize)
	da.SetContentHeight(glyphSize)
	da.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
		draw(cr, w, h)
	})
	b := gtk.NewToggleButton()
	b.SetChild(da)
	b.SetTooltipText(tooltip)
	b.AddCSSClass("transport-btn")
	return b, da
}

func glyphButton(tooltip string, draw func(cr *cairo.Context, w, h int)) (*gtk.Button, *gtk.DrawingArea) {
	da := gtk.NewDrawingArea()
	da.SetContentWidth(glyphSize)
	da.SetContentHeight(glyphSize)
	da.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
		draw(cr, w, h)
	})
	b := gtk.NewButton()
	b.SetChild(da)
	b.SetTooltipText(tooltip)
	b.AddCSSClass("transport-btn")
	return b, da
}

// arrowHead paints a filled triangular head at (x,y) pointing
// along angle a. Pure geometry helper for the glyph set.
func arrowHead(cr *cairo.Context, x, y, a float64) {
	s := 4.2
	cr.MoveTo(x, y)
	cr.LineTo(x-s*math.Cos(a-0.42), y-s*math.Sin(a-0.42))
	cr.LineTo(x-s*math.Cos(a+0.42), y-s*math.Sin(a+0.42))
	cr.ClosePath()
	cr.Fill()
}

// drawShuffle paints two crossing diagonal paths with heads —
// the shuffle gesture, drawn not borrowed.
func (a *app) drawShuffle(cr *cairo.Context, w, h int) {
	r, g, b := glyphColors(a.shuf.on)
	cr.SetSourceRGB(r, g, b)
	cr.SetLineWidth(2)
	cr.SetLineCap(cairo.LineCapRound)
	ang := math.Atan2(10.0, 13.0)
	// upper-left to lower-right
	cr.MoveTo(4, 6)
	cr.LineTo(15.5, 15)
	cr.Stroke()
	arrowHead(cr, 18, 16.5, ang)
	// lower-left to upper-right
	cr.MoveTo(4, 16)
	cr.LineTo(15.5, 7)
	cr.Stroke()
	arrowHead(cr, 18, 5.5, -ang)
}

// drawReshuffle paints a broken ring with a head at the gap —
// a fresh cycle starting now. Distinct from the shuffle cross.
func (a *app) drawReshuffle(cr *cairo.Context, w, h int) {
	r, g, b := glyphColors(false)
	cr.SetSourceRGB(r, g, b)
	cr.SetLineWidth(2)
	cr.SetLineCap(cairo.LineCapRound)
	cr.Arc(11, 11, 7, -0.5, 4.6)
	cr.Stroke()
	arrowHead(cr, 17.2, 8.2, -1.1)
}

// drawEnqueue paints a plus over two register lines — adding to
// what's listed below.
func (a *app) drawEnqueue(cr *cairo.Context, w, h int) {
	r, g, b := glyphColors(false)
	cr.SetSourceRGB(r, g, b)
	cr.SetLineWidth(2)
	cr.SetLineCap(cairo.LineCapRound)
	cr.MoveTo(11, 3.5)
	cr.LineTo(11, 10.5)
	cr.MoveTo(7.5, 7)
	cr.LineTo(14.5, 7)
	cr.Stroke()
	cr.SetLineWidth(1.8)
	cr.MoveTo(5, 15)
	cr.LineTo(17, 15)
	cr.MoveTo(5, 18.5)
	cr.LineTo(13, 18.5)
	cr.Stroke()
}

// drawQueueList paints three register lines with a sounding
// marker — the queue, echoing the ♪ marker language.
func (a *app) drawQueueList(cr *cairo.Context, w, h int) {
	r, g, b := glyphColors(false)
	cr.SetSourceRGB(r, g, b)
	cr.SetLineWidth(2)
	cr.SetLineCap(cairo.LineCapRound)
	cr.MoveTo(7, 6)
	cr.LineTo(17, 6)
	cr.MoveTo(7, 11)
	cr.LineTo(17, 11)
	cr.MoveTo(7, 16)
	cr.LineTo(14, 16)
	cr.Stroke()
	cr.Arc(4, 16, 1.8, 0, 6.2832)
	cr.Fill()
}
