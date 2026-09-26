package main

// viz.go — native spectrum visualizer (Phase 3).
//
// Audio tap: pw-cat records the default sink's monitor source as raw
// PCM (same subprocess spirit as the cmus backend — no PipeWire
// binding). FFT via go-dsp with a Hann window, log-spaced buckets,
// per-bar peak-hold with exponential decay. Colors come from the
// Phase-2 accent engine, never a separate palette.
//
// Lifecycle: the tap + FFT loop run ONLY while playing && focused.
// Anything else (tap failure, pause, blur) lands in a deliberate idle
// or error strip — never a crash, never spam.

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/cmplx"
	"os/exec"
	"sync"

	dspfft "github.com/madelynnblue/go-dsp/fft"
)

const (
	vizRate     = 44100
	vizChannels = 1
	vizFFTSize  = 1024
	vizMinHz    = 60.0
	vizMaxHz    = 14000.0
	vizDecay    = 0.93 // peak multiplier per frame (~25fps)
	vizHeight   = 4    // strip rows; layout reserves exactly this
)

// vizTap owns the pw-cat subprocess and the latest samples.
type vizTap struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	buf     []float64 // latest mono samples in [-1,1], capped
	broken  string
	running bool
}

func defaultMonitor() (string, error) {
	out, err := exec.Command("pactl", "get-default-sink").Output()
	if err != nil {
		return "", fmt.Errorf("pactl: %v", err)
	}
	sink := string(out)
	if i := len(sink); i > 0 && sink[i-1] == '\n' {
		sink = sink[:i-1]
	}
	if sink == "" {
		return "", fmt.Errorf("no default sink")
	}
	return sink + ".monitor", nil
}

func (t *vizTap) start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return
	}
	mon, err := defaultMonitor()
	if err != nil {
		t.broken = err.Error()
		return
	}
	cmd := exec.Command("pw-cat", "-r", "--target", mon,
		"--format", "s16", "--rate", fmt.Sprint(vizRate),
		"--channels", fmt.Sprint(vizChannels), "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.broken = err.Error()
		return
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.broken = err.Error()
		return
	}
	t.cmd = cmd
	t.running = true
	t.broken = ""
	go t.pump(stdout)
}

func (t *vizTap) pump(r io.Reader) {
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			t.mu.Lock()
			for i := 0; i+1 < n; i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(tmp[i:]))) / 32768.0
				t.buf = append(t.buf, v)
			}
			if len(t.buf) > 8192 {
				t.buf = t.buf[len(t.buf)-8192:]
			}
			t.mu.Unlock()
		}
		if err != nil {
			t.mu.Lock()
			t.running = false
			if t.broken == "" {
				t.broken = "tap ended"
			}
			t.mu.Unlock()
			return
		}
	}
}

func (t *vizTap) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Kill AND reap: without Wait the child lingers as a zombie
	// (observed: one zombie per stop cycle).
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_, _ = t.cmd.Process.Wait()
	}
	t.cmd = nil
	t.running = false
	t.buf = nil
}

func (t *vizTap) frame(n int) []float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) < n {
		return nil
	}
	out := make([]float64, n)
	copy(out, t.buf[len(t.buf)-n:])
	return out
}

func (t *vizTap) state() (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running, t.broken
}

// hannWindow applies a Hann envelope in place.
func hannWindow(s []float64) {
	n := len(s)
	for i := range s {
		s[i] *= 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n)))
	}
}

// logEdges returns bars+1 log-spaced bin edges in Hz.
func logEdges(bars int, lo, hi float64) []float64 {
	edges := make([]float64, bars+1)
	ratio := math.Pow(hi/lo, 1/float64(bars))
	for i := range edges {
		edges[i] = lo * math.Pow(ratio, float64(i))
	}
	return edges
}

// fftLevels maps an FFT frame to bars 0..1 levels. dB-mapped against a
// fixed floor so quiet passages sit low without calibration runs.
func fftLevels(samples []float64, rate int, bars int) []float64 {
	out := make([]float64, bars)
	if len(samples) == 0 {
		return out
	}
	hannWindow(samples)
	spec := dspfft.FFTReal(samples)
	n := len(samples)
	edges := logEdges(bars, vizMinHz, vizMaxHz)
	hzPerBin := float64(rate) / float64(n)
	for b := 0; b < bars; b++ {
		loBin := int(edges[b] / hzPerBin)
		hiBin := int(edges[b+1] / hzPerBin)
		if hiBin <= loBin {
			hiBin = loBin + 1
		}
		if loBin >= len(spec) {
			continue
		}
		if hiBin > len(spec) {
			hiBin = len(spec)
		}
		peak := 0.0
		for _, c := range spec[loBin:hiBin] {
			if m := cmplx.Abs(c) / float64(n); m > peak {
				peak = m
			}
		}
		db := 20 * math.Log10(peak+1e-9)
		lvl := (db + 60) / 60
		out[b] = math.Min(1, math.Max(0, lvl))
	}
	return out
}
