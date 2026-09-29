package main

// vizview.go — Phase 2: the TUI's PipeWire/FFT engine (viz.go,
// ported verbatim) driving a native Cairo spectrum strip.
//
// Lifecycle mirrors the TUI: the tap + tick run ONLY while playing,
// healthy, and window-active. Anything else lands on a deliberate
// idle baseline — never a crash, never log spam. Track changes and
// shutdown strictly kill the tap and the tick.

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// vizStripH is the fixed drawing height: compact by design, the art
// owns the panel.
const vizStripH = 90

func hexRGB(hex string) (float64, float64, float64) {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return float64(r) / 255, float64(g) / 255, float64(b) / 255
}

// buildViz creates the drawing area and wires the Cairo draw func.
// Bars read app levels/peaks live; colors read the current track
// accents with theme fallback — same pairing as the TUI strip.
func (a *app) buildViz(parent *gtk.Box) {
	a.viz = gtk.NewDrawingArea()
	a.viz.SetContentHeight(vizStripH)
	a.viz.SetHExpand(true)
	a.viz.AddCSSClass("viz-strip")
	a.viz.SetDrawFunc(func(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
		a.drawViz(cr, w, h)
	})
	parent.Append(a.viz)
}

// vizBars fits the bar count to the allocated width: column+gutter.
func vizBars(w int) int {
	n := w / 16
	if n < 8 {
		n = 8
	}
	if n > 48 {
		n = 48
	}
	return n
}

// drawViz paints one frame: gradient bars with round caps plus
// peak-hold dots, or the resting baseline when the loop is off.
func (a *app) drawViz(cr *cairo.Context, w, h int) {
	a.vizW = w
	prim, sec := a.artPrim, a.artSec
	if prim == "" {
		prim = a.cfg.Theme.Accent
	}
	if sec == "" {
		sec = a.cfg.Theme.Accent2
	}
	if !a.vizActive || len(a.levels) == 0 {
		// Resting baseline, not missing: one quiet line in the
		// live accent at low alpha.
		r, g, b := hexRGB(prim)
		cr.SetSourceRGBA(r, g, b, 0.35)
		cr.SetLineWidth(2)
		cr.MoveTo(0, float64(h)-2)
		cr.LineTo(float64(w), float64(h)-2)
		cr.Stroke()
		return
	}
	n := vizBars(w)
	pr, pg, pb := hexRGB(prim)
	sr, sg, sb := hexRGB(sec)
	grad, err := cairo.NewPatternLinear(0, float64(h), 0, 0)
	if err == nil {
		grad.AddColorStopRGB(0, pr, pg, pb)
		grad.AddColorStopRGB(1, sr, sg, sb)
		cr.SetSource(grad)
	} else {
		cr.SetSourceRGB(pr, pg, pb)
	}
	cr.SetLineCap(cairo.LineCapRound)
	colW := float64(w) / float64(n)
	barW := colW * 0.55
	if barW < 2 {
		barW = 2
	}
	cr.SetLineWidth(barW)
	for i := 0; i < n && i*int(colW) < w; i++ {
		lvl := 0.0
		if i < len(a.levels) {
			lvl = a.levels[i]
		}
		peak := 0.0
		if i < len(a.peaks) {
			peak = a.peaks[i]
		}
		x := float64(i)*colW + colW/2
		top := float64(h) - 3 - lvl*float64(h-6)
		if top < 2 {
			top = 2
		}
		cr.MoveTo(x, float64(h)-3)
		cr.LineTo(x, top)
		cr.Stroke()
		if peak > lvl {
			py := float64(h) - 3 - peak*float64(h-6)
			if py < 3 {
				py = 3
			}
			cr.Arc(x, py, 2, 0, 6.2832)
			cr.Fill()
		}
	}
}

// wantViz reports whether the loop should run: playing, healthy,
// and window-active. One predicate, checked on every tick and poll.
func (a *app) wantViz() bool {
	return a.status.State == "playing" && a.beErr == "" &&
		a.win != nil && a.win.IsActive()
}

// ensureViz starts or stops the tap + tick to match wantViz.
// Idempotent: steady state is a no-op.
func (a *app) ensureViz() {
	if a.wantViz() && !a.vizActive {
		a.tap.start()
		if _, broken := a.tap.state(); broken != "" {
			// Fail-safe: surface once as state, render idle.
			// No retry spin, no log spam; next poll re-checks.
			a.vizErr = broken
			a.tap.stop()
			a.vizActive = false
			return
		}
		a.vizErr = ""
		a.vizActive = true
		a.levels, a.peaks = nil, nil
		if a.vizTick == 0 {
			a.vizTick = glib.TimeoutAdd(40, func() bool { return a.tickViz() })
		}
		return
	}
	if !a.wantViz() && a.vizActive {
		// Demo owns its seeded display state (no tick, no tap);
		// the idle teardown must not clear it every poll.
		if a.demoHold {
			return
		}
		a.stopViz()
	}
}

// restartTap kills and restarts the tap on track transitions:
// no stale buffered audio from the previous track, no orphans.
func (a *app) restartTap() {
	if a.vizTick != 0 {
		glib.SourceRemove(a.vizTick)
		a.vizTick = 0
	}
	a.tap.stop()
	a.vizActive = false
	a.levels, a.peaks = nil, nil
	a.ensureViz()
}

// stopViz halts tap + tick and settles one idle frame.
func (a *app) stopViz() {
	if a.vizTick != 0 {
		glib.SourceRemove(a.vizTick)
		a.vizTick = 0
	}
	a.tap.stop()
	a.vizActive = false
	a.levels, a.peaks = nil, nil
	if a.viz != nil {
		a.viz.QueueDraw()
	}
}

// tickViz is the 40ms frame: health-check, FFT, peaks, repaint.
// Returning false unschedules; ensureViz re-arms on next poll.
func (a *app) tickViz() bool {
	if !a.wantViz() {
		a.stopViz()
		return false
	}
	if running, broken := a.tap.state(); !running {
		a.vizErr = broken
		a.stopViz()
		return false
	}
	if frame := a.tap.frame(vizFFTSize); frame != nil {
		bars := vizBars(a.vizW)
		if bars < 8 {
			bars = 32
		}
		a.levels = fftLevels(frame, vizRate, bars)
		if len(a.peaks) != len(a.levels) {
			a.peaks = make([]float64, len(a.levels))
		}
		for i, l := range a.levels {
			if l > a.peaks[i] {
				a.peaks[i] = l
			} else {
				a.peaks[i] *= vizDecay
			}
		}
	}
	a.viz.QueueDraw()
	return true
}
