package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func solidImage(w, h int, r, g, b uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}

func TestHalfBlockDeterministic(t *testing.T) {
	// 1 col x 1 row: top red, bottom blue.
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(0, 1, color.RGBA{0, 0, 255, 255})
	got := renderHalfBlock(img, 1, 1)
	want := "\x1b[38;2;255;0;0m\x1b[48;2;0;0;255m▀\x1b[0m\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAccentPairSolid(t *testing.T) {
	prim, _ := accentPair(solidImage(8, 8, 255, 0, 0))
	if prim != "#ff0000" {
		t.Fatalf("solid red -> %q, want #ff0000", prim)
	}
}

func TestAccentPairGrayFallsBack(t *testing.T) {
	prim, sec := accentPair(solidImage(8, 8, 120, 120, 120))
	if prim != "" || sec != "" {
		t.Fatalf("gray should yield no accent, got %q %q", prim, sec)
	}
}

func TestKittyFramesFormat(t *testing.T) {
	payload := make([]byte, 9000)
	for i := range payload {
		payload[i] = byte(i)
	}
	frames := kittyPNGFrames(payload, 10, 10, 7)
	if len(frames) != 4 { // 3 data chunks (4096+4096+808) + display
		t.Fatalf("got %d frames, want 4", len(frames))
	}
	if !strings.Contains(frames[0], "a=t,i=7") || !strings.Contains(frames[0], "m=1;") {
		t.Fatalf("first frame wrong: %.60s", frames[0])
	}
	if !strings.Contains(frames[2], "m=0;") {
		t.Fatalf("last data frame must have m=0: %.60s", frames[2])
	}
	if !strings.Contains(frames[3], "a=p,i=7") {
		t.Fatalf("display frame wrong: %.60s", frames[3])
	}
	for _, f := range frames {
		if !strings.HasSuffix(f, "\x1b\\") {
			t.Fatalf("frame missing APC terminator: %.40s", f)
		}
	}
}

func TestArtCoverage(t *testing.T) {
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	withArt := 0
	for _, tr := range tracks {
		raw, err := rawArtBytes(tr.Path)
		if err != nil || len(raw) == 0 {
			t.Logf("NOART: %s", tr.Path)
			continue
		}
		withArt++
	}
	t.Logf("art present in %d/%d tracks", withArt, len(tracks))
	if withArt*100/len(tracks) < 95 {
		t.Fatalf("art coverage below 95%%: %d/%d", withArt, len(tracks))
	}
}

func firstTrackWithArt(t *testing.T, tracks []Track) string {
	t.Helper()
	for _, tr := range tracks {
		if raw, err := rawArtBytes(tr.Path); err == nil && len(raw) > 0 {
			return tr.Path
		}
	}
	t.Fatal("no track with art found")
	return ""
}

func TestCachedArtRoundTrip(t *testing.T) {
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	path := firstTrackWithArt(t, tracks)
	img1, p1, s1, err := cachedArt(path)
	if err != nil || img1 == nil {
		t.Fatalf("first miss: %v", err)
	}
	img2, p2, s2, err := cachedArt(path)
	if err != nil || img2 == nil {
		t.Fatalf("second hit: %v", err)
	}
	if p1 != p2 || s1 != s2 {
		t.Fatalf("accent unstable: %q/%q vs %q/%q", p1, s1, p2, s2)
	}
	if img1.Bounds().Dx() != img2.Bounds().Dx() {
		t.Fatalf("cached dims unstable")
	}
}

func TestProbeNeverHangs(t *testing.T) {
	// A terminal that accepts the query but never replies must still
	// yield false quickly. (This exact hang wedged startup once.)
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer master.Close()
	done := make(chan bool, 1)
	go func() { done <- probeConn(slave) }()
	select {
	case r := <-done:
		if r {
			t.Fatalf("silent terminal reported Kitty support")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probeConn hung >5s on silent terminal")
	}
}

func TestBoxBlurSmooths(t *testing.T) {
	// Half-black/half-white: blur must pull edge pixels toward gray.
	img := image.NewRGBA(image.Rect(0, 0, 40, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			if x < 20 {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	out := boxBlurPass(img, 8)
	r, _, _, _ := out.RGBAAt(19, 5).RGBA()
	got := int(r >> 8)
	if got < 40 || got > 215 {
		t.Fatalf("edge pixel not smoothed: %d", got)
	}
	r0, _, _, _ := out.RGBAAt(0, 5).RGBA()
	if int(r0>>8) > 30 {
		t.Fatalf("deep interior leaked: %d", int(r0>>8))
	}
}

func TestScrimContrast(t *testing.T) {
	// Bright art scrims toward dark bg: text must stay readable.
	white := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			white.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	// Worst case (pure-white art) must clear the large-text floor of
	// 3.0. Real-library worst case measured 3.59 — the known tension
	// between readability and visible art, bounded, not papered over.
	scr := scrimToward(white, "#161310", 0.35)
	r, g, b, _ := scr.RGBAAt(4, 4).RGBA()
	got := fmt.Sprintf("#%02x%02x%02x", int(r>>8), int(g>>8), int(b>>8))
	if ratio := contrastRatio("#EDE0C8", got); ratio < 3.0 {
		t.Fatalf("contrast %.2f below 3.0 on %s", ratio, got)
	}
}

func TestBlurPerf(t *testing.T) {
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	img, _, _, err := cachedArt(firstTrackWithArt(t, tracks))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = blurCached(toRGBA(img))
	// Threshold is generous (race detector slows everything ~10x);
	// it catches pathological blowups, not benchmarks.
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("blur too slow: %v", d)
	} else {
		t.Logf("3-pass blur: %v", d)
	}
}

func TestBgRender(t *testing.T) {
	cfg := defaultConfig()
	cfg.Theme.BackgroundArt = true
	m := newModel(&backend{sock: "/nonexistent.sock"}, false, cfg)
	m.width, m.height = 167, 39
	m.layoutPanes()
	m.status = Status{State: "playing", File: "/x.flac", Artist: "A", Title: "T", Album: "Al", Duration: 200, Position: 10}
	tracks, err := indexLibrary()
	if err != nil {
		t.Fatal(err)
	}
	path := firstTrackWithArt(t, tracks)
	m.loadArt(path)
	if m.bgImg == nil {
		t.Fatal("no bg image built")
	}
	if _, rows := m.artBox(); rows != 0 {
		t.Fatalf("bg mode must reserve zero art rows, got %d", rows)
	}
	out := m.renderBgText()
	if !strings.Contains(out, "T") || !strings.Contains(out, "48;2") {
		t.Fatalf("bg text missing content or background codes")
	}
}

func TestBgTintBand(t *testing.T) {
	// Any input hue lands in a narrow dark band, never raw brightness.
	for _, in := range []string{"#ffffff", "#ff0000", "#cba6f7", "#94e2d5", "#11111b"} {
		got := bgTint(in)
		var r, g, b int
		fmt.Sscanf(got, "#%02x%02x%02x", &r, &g, &b)
		_, _, v := rgbToHsv(float64(r)/255, float64(g)/255, float64(b)/255)
		if v < 0.06 || v > 0.17 {
			t.Fatalf("bgTint(%s) = %s lightness out of band (v=%.2f)", in, got, v)
		}
		if ratio := contrastRatio("#EDE0C8", got); ratio < 4.5 {
			t.Fatalf("bgTint(%s) = %s contrast %.2f below 4.5", in, got, ratio)
		}
	}
}

// stripeImage builds a hue-diverse image: nHues vertical stripes at
// fixed saturation/value. Models busy multicolor art (e.g. neon on
// dark) where no single hue bucket holds a majority.
func stripeImage(w, h, nHues int, sat, val float64) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			hue := float64(x*nHues/w) / float64(nHues)
			r, g, b := hsvToRgb(hue, sat, val)
			img.SetRGBA(x, y, color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255})
		}
	}
	return img
}

func TestAccentPairMulticolorThemes(t *testing.T) {
	// 12 dark saturated hues: every bucket scores below the 0.04
	// dominance floor, but overall art is colorful (mean S = 0.6).
	prim, _ := accentPair(stripeImage(144, 144, 12, 0.6, 0.35))
	if prim == "" {
		t.Fatalf("multicolor art fell back to Mocha instead of theming")
	}
}

func TestAccentPairMutedMulticolorStillFallsBack(t *testing.T) {
	// Same hue diversity, genuinely desaturated: fallback is correct.
	prim, sec := accentPair(stripeImage(144, 144, 12, 0.05, 0.35))
	if prim != "" || sec != "" {
		t.Fatalf("muted art should fall back, got %q %q", prim, sec)
	}
}

func TestAccentPair1800Themes(t *testing.T) {
	// Reference case: bbno$ & Ironmouse – 1-800. Dark, hue-diverse,
	// moderately saturated art that fell back before the trigger fix.
	os.Setenv("HOME", "/home/miz")
	img, prim, _, err := cachedArt("/home/miz/FLAC/01 - bbno$ & Ironmouse - 1-800.flac")
	if err != nil || img == nil {
		t.Skipf("reference art unavailable: %v", err)
	}
	if prim == "" {
		t.Fatalf("1-800 art fell back instead of theming")
	}
}

func TestRevealMask(t *testing.T) {
	img := stripeImage(144, 144, 6, 0.8, 0.8)
	full := renderHalfBlock(img, 24, 8)
	masked0 := renderHalfBlockMasked(img, 24, 8, 0)
	if strings.Contains(masked0, "▀") {
		t.Fatalf("zero-progress wipe must paint nothing")
	}
	if strings.Count(masked0, "\n") != strings.Count(full, "\n") {
		t.Fatalf("wipe must hold row count: %d vs %d", strings.Count(masked0, "\n"), strings.Count(full, "\n"))
	}
	if got := renderHalfBlockMasked(img, 24, 8, 24); got != full {
		t.Fatalf("full-progress wipe must equal the plain render")
	}
	half := renderHalfBlockMasked(img, 24, 8, 12)
	if n := strings.Count(half, "▀"); n != 8*12 {
		t.Fatalf("half wipe painted %d cells, want %d", n, 8*12)
	}
}
