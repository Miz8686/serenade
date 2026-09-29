package main

import (
	"fmt"
	"os"
	"testing"
)

func TestBackdropContrast(t *testing.T) {
	os.Setenv("HOME", "/home/miz")
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	base := defaultConfig().Theme
	fg := []struct {
		name  string
		hex   string
		floor float64
	}{
		{"paper", base.Text, 7.0},
		{"brass", base.Accent, 4.5},
		{"verdigris", base.Accent2, 4.0},
		{"muted", base.Muted, 3.0},
	}
	worst := map[string]float64{}
	worstPath := map[string]string{}
	for _, f := range fg {
		worst[f.name] = 99
	}
	n := 0
	for _, tr := range tracks {
		img, _, _, err := cachedArt(tr.Path)
		if err != nil || img == nil {
			continue
		}
		n++
		bg := scrimAdaptive(smallBlur(toRGBA(img)), base.Bg)
		b := bg.Bounds()
		for y := 0; y < b.Dy(); y += 2 {
			for x := 0; x < b.Dx(); x += 2 {
				r, g, bl, _ := bg.RGBAAt(x, y).RGBA()
				hex := fmt.Sprintf("#%02x%02x%02x", int(r>>8), int(g>>8), int(bl>>8))
				for _, f := range fg {
					if c := contrastRatio(f.hex, hex); c < worst[f.name] {
						worst[f.name], worstPath[f.name] = c, tr.Path
					}
				}
			}
		}
	}
	for _, f := range fg {
		t.Logf("%s worst %.2f (%s)", f.name, worst[f.name], worstPath[f.name])
		if worst[f.name] < f.floor {
			t.Fatalf("%s contrast %.2f below %.1f", f.name, worst[f.name], f.floor)
		}
	}
	t.Logf("backdrop cover over %d tracks", n)
}
