package main

import (
	"image"
	"image/color"
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
