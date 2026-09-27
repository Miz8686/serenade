package main

import (
	"os"
	"testing"
)

func shotModel(t *testing.T, file string, playIdx int) model {
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	m.tracks, m.view = tracks, tracks
	m.indexing = false
	m.width, m.height = 167, 39
	m.layoutPanes()
	if file != "" {
		var tr Track
		for _, x := range tracks {
			if x.Path == file {
				tr = x
			}
		}
		m.status = Status{State: "playing", File: file, Artist: tr.Artist, Title: tr.Title, Album: tr.Album, Duration: 184, Position: 61}
		m.loadArt(file)
		m.cursor = playIdx
		m.followPlaying(file)
		// lively synthetic spectrum
		n := m.vizBars()
		m.vizActive = true
		m.levels = make([]float64, n)
		m.peaks = make([]float64, n)
		for i := range m.levels {
			m.levels[i] = 0.25 + 0.65*float64((i*37+11)%10)/10.0
			m.peaks[i] = m.levels[i] + 0.08
		}
		m.syncDetail()
	}
	m.clampCursor()
	return m
}

func TestShots(t *testing.T) {
	idle := newModel(&backend{sock: "/nonexistent.sock"}, false, defaultConfig())
	os.Setenv("HOME", "/home/miz")
	tr, _ := indexLibrary()
	idle.tracks, idle.view = tr, tr
	idle.indexing = false
	idle.width, idle.height = 167, 39
	idle.layoutPanes()
	idle.clampCursor()
	os.WriteFile("/tmp/shot_idle.txt", []byte(idle.View()), 0o644)

	p := shotModel(t, "/home/miz/Phone-backup/davinci-20260925/4a/FLAC/Linkin Park - Papercut.flac", 0)
	os.WriteFile("/tmp/shot_papercut.txt", []byte(p.View()), 0o644)

	b := shotModel(t, "/home/miz/FLAC/01 - bbno$ & Ironmouse - 1-800.flac", 0)
	os.WriteFile("/tmp/shot_1800.txt", []byte(b.View()), 0o644)

	f := shotModel(t, "/home/miz/Sparsha FLACS/03 - Chirag Khadka - Samadhi.flac", 0)
	os.WriteFile("/tmp/shot_fallback.txt", []byte(f.View()), 0o644)
}
