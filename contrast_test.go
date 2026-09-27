package main

import (
	"fmt"
	"testing"
)

func TestRealArtContrast(t *testing.T) {
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	min, worst := 99.0, ""
	n := 0
	for _, tr := range tracks {
		img, _, _, err := cachedArt(tr.Path)
		if err != nil || img == nil {
			continue
		}
		scr := scrimToward(toRGBA(img), "#161310", 0.35)
		// worst pixel: sample a grid, take min contrast vs text
		b := scr.Bounds()
		local := 99.0
		for y := 0; y < b.Dy(); y += 10 {
			for x := 0; x < b.Dx(); x += 10 {
				r, g, bl, _ := scr.RGBAAt(x, y).RGBA()
				hex := fmt.Sprintf("#%02x%02x%02x", int(r>>8), int(g>>8), int(bl>>8))
				if c := contrastRatio("#EDE0C8", hex); c < local {
					local = c
				}
			}
		}
		n++
		if local < min {
			min, worst = local, tr.Path
		}
	}
	t.Logf("sampled %d arts, worst-cell contrast %.2f (%s)", n, min, worst)
	if min < 3.0 {
		t.Fatalf("contrast below large-text floor: %.2f", min)
	}
}

// TestBodyTextVsDynamicBg extends coverage globally: body text against
// the per-track dynamic background tint (not just the Now Playing
// scrim case above). Dark band guarantees the higher 4.5 bar.
func TestBodyTextVsDynamicBg(t *testing.T) {
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	checked, worst, worstPath := 0, 99.0, ""
	for _, tr := range tracks {
		img, _, _, err := cachedArt(tr.Path)
		if err != nil || img == nil {
			continue
		}
		prim, _ := accentPair(img)
		if prim == "" {
			prim = "#D9A44C"
		}
		bg := bgTint(prim)
		c := contrastRatio("#EDE0C8", bg)
		checked++
		if c < worst {
			worst, worstPath = c, tr.Path
		}
	}
	t.Logf("body text vs dynamic bg over %d tracks: worst %.2f (%s)", checked, worst, worstPath)
	if worst < 4.5 {
		t.Fatalf("body contrast below 4.5: %.2f", worst)
	}
}
