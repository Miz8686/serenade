package main

import (
	"math"
	"testing"
)

func TestFFTPeakBin(t *testing.T) {
	// Synthetic 440Hz tone must land its peak in the matching
	// log bucket: proves the ported engine measures real spectra.
	n := 1024
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/44100)
	}
	levels := fftLevels(samples, 44100, 24)
	peak, peakI := -1.0, -1
	for i, l := range levels {
		if l > peak {
			peak, peakI = l, i
		}
	}
	// 440Hz in 60..14000 log buckets (24 bars) lands around bar 8-9.
	if peakI < 6 || peakI > 11 {
		t.Fatalf("440Hz peak at bar %d, want 6..11", peakI)
	}
	if peak < 0.3 {
		t.Fatalf("peak level too low: %.2f", peak)
	}
}

func TestSilenceFlat(t *testing.T) {
	levels := fftLevels(make([]float64, 1024), 44100, 24)
	for i, l := range levels {
		if l != 0 {
			t.Fatalf("silence bar %d = %.3f", i, l)
		}
	}
}

func TestVizBars(t *testing.T) {
	if vizBars(100) != 8 || vizBars(10000) != 48 {
		t.Fatalf("bar count clamping broken")
	}
	if vizBars(512) != 32 {
		t.Fatalf("512px -> %d bars, want 32", vizBars(512))
	}
}

func TestHexRGB(t *testing.T) {
	r, g, b := hexRGB("#D9A44C")
	if r < 0.84 || r > 0.86 || g < 0.63 || g > 0.65 || b < 0.29 || b > 0.31 {
		t.Fatalf("hex parse: %.2f %.2f %.2f", r, g, b)
	}
}
