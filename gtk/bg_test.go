package main

import (
	"fmt"
	"image"
	"image/color"
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

// TestScrimKeepsPhotoHue pins the saturation-aware scrim: neutral
// photographic gray must darken WITHOUT taking the ink's warm cast
// (the generic dark-mud failure), while saturated art keeps the
// warm ink blend (atmosphere, as before).
func TestScrimKeepsPhotoHue(t *testing.T) {
	base := defaultConfig().Theme.Bg
	mk := func(c color.RGBA) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				img.SetRGBA(x, y, c)
			}
		}
		return img
	}
	gray := scrimAdaptive(mk(color.RGBA{128, 128, 128, 255}), base)
	r, g, b, _ := gray.RGBAAt(4, 4).RGBA()
	fr, fg, fb := float64(r>>8), float64(g>>8), float64(b>>8)
	if d := maxF(maxF(fr, fg), fb) - minF(minF(fr, fg), fb); d > 14 {
		t.Fatalf("gray photo pixel went muddy warm: #%02x%02x%02x (spread %.0f)", int(fr), int(fg), int(fb), d)
	}
	if lum := 0.2126*fr/255 + 0.7152*fg/255 + 0.0722*fb/255; lum > 0.083 {
		t.Fatalf("gray scrim too bright for paper text: lum %.3f", lum)
	}
	red := scrimAdaptive(mk(color.RGBA{220, 30, 30, 255}), base)
	rr, rg, rb, _ := red.RGBAAt(4, 4).RGBA()
	if float64(rr>>8) <= float64(rg>>8)+10 {
		t.Fatalf("saturated art lost its character: #%02x%02x%02x", int(rr>>8), int(rg>>8), int(rb>>8))
	}
}
