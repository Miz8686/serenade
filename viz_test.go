package main

import (
	"math"
	"testing"
)

// sineFrame synthesizes N samples of a pure tone for pipeline tests.
func sineFrame(freq float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Sin(2 * math.Pi * freq * float64(i) / vizRate)
	}
	return out
}

func TestFFTPeakBin(t *testing.T) {
	levels := fftLevels(sineFrame(440, vizFFTSize), vizRate, 24)
	best, bi := -1.0, -1
	for i, l := range levels {
		if l > best {
			best, bi = l, i
		}
	}
	// 440Hz in 60..14000 log buckets (24 bars) lands around bar 8-9.
	if bi < 6 || bi > 11 {
		t.Fatalf("440Hz peak at bar %d, want 6..11", bi)
	}
	if best < 0.3 {
		t.Fatalf("peak level too low: %f", best)
	}
}

func TestPeakDecay(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.levels = []float64{0.1, 0.1}
	m.peaks = []float64{0.9, 0.9}
	for i, l := range m.levels {
		if l > m.peaks[i] {
			m.peaks[i] = l
		} else {
			m.peaks[i] *= vizDecay
		}
	}
	if m.peaks[0] >= 0.9 || m.peaks[0] <= 0.1 {
		t.Fatalf("peak should decay toward level, got %f", m.peaks[0])
	}
}

func TestLogEdges(t *testing.T) {
	edges := logEdges(24, vizMinHz, vizMaxHz)
	if len(edges) != 25 || edges[0] != vizMinHz || math.Abs(edges[24]-vizMaxHz) > 1e-6 {
		t.Fatalf("edges wrong: %v..%v len %d", edges[0], edges[len(edges)-1], len(edges))
	}
	r0 := edges[1] / edges[0]
	for i := 1; i < len(edges)-1; i++ {
		if r := edges[i+1] / edges[i]; math.Abs(r-r0) > 1e-9 {
			t.Fatalf("not log-spaced at %d: %f vs %f", i, r, r0)
		}
	}
}

func TestSilenceFlat(t *testing.T) {
	levels := fftLevels(make([]float64, vizFFTSize), vizRate, 24)
	for i, l := range levels {
		if l != 0 {
			t.Fatalf("silence bar %d = %f, want 0", i, l)
		}
	}
}

func TestMonitorResolve(t *testing.T) {
	mon, err := defaultSink()
	if err != nil {
		t.Skipf("no pactl sink here: %v", err)
	}
	if !contains(mon, ".monitor") {
		t.Fatalf("not a monitor source: %q", mon)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestStripRendersLiveLevels(t *testing.T) {
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.width, m.height = 167, 39
	m.vizActive = true
	m.levels = make([]float64, 24)
	m.peaks = make([]float64, 24)
	for i := range m.levels {
		m.levels[i] = 0.8
		m.peaks[i] = 0.9
	}
	out := m.renderViz()
	blocks, peaks := 0, 0
	for _, r := range out {
		switch r {
		case '█':
			blocks++
		case '─':
			peaks++
		}
	}
	if blocks == 0 || peaks == 0 {
		t.Fatalf("strip missing bars (%d) or peaks (%d)", blocks, peaks)
	}
	lines := 0
	for _, c := range out {
		if c == '\n' {
			lines++
		}
	}
	if lines != vizHeight {
		t.Fatalf("strip height %d, want %d", lines, vizHeight)
	}
}
