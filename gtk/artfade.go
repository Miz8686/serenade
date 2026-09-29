package main

// artfade.go — item 9: track-change crossfade on the art/backdrop
// swap. Nothing more this round.
//
// Plain GTK4 only: widget opacity stepped on a ~16ms glib timeout
// (frame-clock-driven custom animation, no libadwaita, no CSS
// transition on paintables — a Revealer twin-stack would double
// texture memory per swap for no gain).
//
// Shape: fade out (~150ms) → swap paintables → fade in (~180ms).
// A generation counter makes rapid-fire safe: every loadArt bumps
// artGen, and each step closure captures its gen and aborts on
// mismatch, so a fast skip sequence can never leave a stale fade
// writing opacity or landing an old texture on top. The viz
// DrawingArea is untouched — separate paint path, no interaction
// with the blur backdrop bake (which still happens exactly once
// per track, synchronously, before the fade).

import (
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// fadeOutMs / fadeInMs bound the two phases; steps run every
// fadeTickMs. Kept as consts (not config) — one scope, no knobs.
const (
	fadeTickMs = 16
	fadeOutMs  = 150
	fadeInMs   = 180
)

// fadeGen is the overlap guard. next starts a new fade, live
// reports whether gen is still the newest. Pure for testing the
// rapid-fire edge without a display.
type fadeGen struct{ cur int }

func (g *fadeGen) next() int {
	g.cur++
	return g.cur
}

func (g *fadeGen) live(gen int) bool { return gen == g.cur }

// fadeOpacity steps from→to over ms, ticking every fadeTickMs.
// onStep applies; done fires once at the end. Stale gens abort
// silently at the next tick — no partial writes after a skip.
func (a *app) fadeOpacity(gen int, from, to float64, ms int, onStep func(float64), done func()) {
	steps := ms / fadeTickMs
	if steps < 1 {
		steps = 1
	}
	i := 0
	var tick func() bool
	tick = func() bool {
		if !a.fGen.live(gen) {
			return false
		}
		i++
		t := float64(i) / float64(steps)
		if t >= 1 {
			onStep(to)
			if done != nil {
				done()
			}
			return false
		}
		onStep(from + (to-from)*t)
		glib.TimeoutAdd(uint(fadeTickMs), tick)
		return false
	}
	glib.TimeoutAdd(uint(fadeTickMs), tick)
}

// fadeSwap runs swap between a fade-out and fade-in on both the
// sharp art and the blurred backdrop together, so they read as
// one atmosphere change instead of two treatments cutting at
// different times (the seam concern from item 1, in motion).
func (a *app) fadeSwap(gen int, swap func()) {
	set := func(op float64) {
		if a.art != nil {
			a.art.SetOpacity(op)
		}
		if a.bgPic != nil {
			a.bgPic.SetOpacity(op)
		}
	}
	if a.art == nil || a.bgPic == nil {
		swap()
		return
	}
	start := 1.0
	if o := a.art.Opacity(); o < start {
		start = o // rapid-fire: continue from mid-fade, never pop
	}
	a.fadeOpacity(gen, start, 0, fadeOutMs, set, func() {
		if !a.fGen.live(gen) {
			return
		}
		swap()
		a.fadeOpacity(gen, 0, 1, fadeInMs, set, nil)
	})
}
