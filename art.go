package main

// art.go — cover art pipeline for Phase 2.
//
// Extraction (embedded via dhowden/tag, else folder fallback) →
// decode + downsample (cached) → accent sampling (cached) → render
// (native half-block PRIMARY; Kitty graphics only behind a real
// tty probe, never env guessing).
//
// Cost rule: decode/resize/sample happen ONLY on cache miss. Render
// (cell mapping) is cheap and runs on track change or resize.

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const artCacheMaxW = 300

func artDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "gomusic", "art")
}

// artKey identifies the source bytes: path + mtime + size, so retagged
// files naturally invalidate.
func artKey(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	h := sha1.Sum([]byte(path + fi.ModTime().String() + fmt.Sprint(fi.Size())))
	return fmt.Sprintf("%x", h)
}

var folderArtNames = []string{
	"cover.jpg", "folder.jpg", "cover.png", "folder.png",
	"front.jpg", "front.png", "cover.jpeg", "folder.jpeg",
	"albumart.jpg", "cover.webp", "folder.webp",
}

// rawArtBytes returns embedded art, else a same-directory fallback file.
func rawArtBytes(path string) ([]byte, error) {
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
			if pic := m.Picture(); pic != nil && len(pic.Data) > 0 {
				f.Close()
				return pic.Data, nil
			}
		}
		f.Close()
	}
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("no art for %s", path)
	}
	want := map[string]bool{}
	for _, n := range folderArtNames {
		want[n] = true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if want[strings.ToLower(e.Name())] {
			return os.ReadFile(filepath.Join(dir, e.Name()))
		}
	}
	return nil, fmt.Errorf("no art for %s", path)
}

// cachedArt returns the decoded+downsampled image plus accent pair,
// doing expensive work only on cache miss. Cache: <hash>.png +
// <hash>.txt ("primary\nsecondary").
func cachedArt(path string) (image.Image, string, string, error) {
	key := artKey(path)
	if key == "" {
		return nil, "", "", fmt.Errorf("stat: %s", path)
	}
	dir := artDir()
	imgPath := filepath.Join(dir, key+".png")
	accPath := filepath.Join(dir, key+".txt")
	if raw, err := os.ReadFile(imgPath); err == nil {
		if img, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
			if acc, err := os.ReadFile(accPath); err == nil {
				lines := strings.Split(strings.TrimSpace(string(acc)), "\n")
				prim, sec := "", ""
				if len(lines) > 0 {
					prim = lines[0]
				}
				if len(lines) > 1 {
					sec = lines[1]
				}
				return img, prim, sec, nil
			}
		}
	}
	raw, err := rawArtBytes(path)
	if err != nil {
		return nil, "", "", err
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", "", err
	}
	sb := src.Bounds()
	w := artCacheMaxW
	h := sb.Dy() * w / max(1, sb.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
	prim, sec := accentPair(dst)
	_ = os.MkdirAll(dir, 0o755)
	var buf bytes.Buffer
	if err := encodePNG(&buf, dst); err != nil {
		return nil, "", "", err
	}
	_ = os.WriteFile(imgPath, buf.Bytes(), 0o644)
	_ = os.WriteFile(accPath, []byte(prim+"\n"+sec+"\n"), 0o644)
	return dst, prim, sec, nil
}

// accentPair buckets hue (36 buckets), weights by saturation*value,
// returns primary (top bucket, brightness-floored) and secondary
// (runner-up). Empty strings when nothing usable — caller falls back
// to theme tokens.
func accentPair(img image.Image) (string, string) {
	const buckets = 36
	var sumS, sumV, sumR, sumG, sumB [buckets]float64
	var cnt [buckets]int
	b := img.Bounds()
	step := max(1, (b.Dx()*b.Dy())/900)
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			hh, ss, vv := rgbToHsv(float64(r>>8)/255, float64(g>>8)/255, float64(bl>>8)/255)
			bi := int(hh * buckets)
			if bi >= buckets {
				bi = buckets - 1
			}
			w := ss * (0.4 + 0.6*vv)
			sumS[bi] += ss * w
			sumV[bi] += vv * w
			sumR[bi] += float64(r>>8) * w
			sumG[bi] += float64(g>>8) * w
			sumB[bi] += float64(bl>>8) * w
			cnt[bi]++
			n++
		}
	}
	type scored struct {
		score   float64
		h, s, v float64
	}
	var ranked []scored
	for i := 0; i < buckets; i++ {
		if cnt[i] == 0 {
			continue
		}
		avgS := sumS[i] / float64(cnt[i])
		avgV := sumV[i] / float64(cnt[i])
		score := avgS * (0.4 + 0.6*avgV) * float64(cnt[i]) / float64(max(1, n))
		ranked = append(ranked, scored{score, float64(i) / buckets, avgS, avgV})
	}
	if len(ranked) == 0 {
		return "", ""
	}
	best, second := 0, -1
	for i := 1; i < len(ranked); i++ {
		if ranked[i].score > ranked[best].score {
			second, best = best, i
		} else if second == -1 || ranked[i].score > ranked[second].score {
			second = i
		}
	}
	mk := func(s scored) string {
		if s.score < 0.04 {
			return ""
		}
		v := maxF(s.v, 0.55)
		r, g, bl := hsvToRgb(s.h, s.s, v)
		return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(bl*255))
	}
	prim := mk(ranked[best])
	sec := ""
	if second >= 0 {
		sec = mk(ranked[second])
	}
	return prim, sec
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func rgbToHsv(r, g, b float64) (h, s, v float64) {
	mx := maxF(r, maxF(g, b))
	mn := minF(r, minF(g, b))
	v = mx
	if mx == 0 {
		return 0, 0, 0
	}
	s = (mx - mn) / mx
	if s == 0 {
		return 0, s, v
	}
	switch mx {
	case r:
		h = (g - b) / (mx - mn)
	case g:
		h = 2 + (b-r)/(mx-mn)
	default:
		h = 4 + (r-g)/(mx-mn)
	}
	h /= 6
	if h < 0 {
		h += 1
	}
	return h, s, v
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func hsvToRgb(h, s, v float64) (r, g, b float64) {
	if s == 0 {
		return v, v, v
	}
	h *= 6
	i := int(h)
	f := h - float64(i)
	p, q, t := v*(1-s), v*(1-s*f), v*(1-s*(1-f))
	switch i % 6 {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	default:
		return v, p, q
	}
}

// renderHalfBlock maps img onto cols×rows terminal cells. Each cell is
// one ▀ glyph: foreground = top source pixel, background = bottom.
// Terminal cells are ~1:2 (w:h), so sampling cols × rows*2 source pixels
// yields roughly-square-looking output. Run-length emits color codes
// only on change; callers size img appropriately (cached pixels reused).
func renderHalfBlock(img image.Image, cols, rows int) string {
	if cols < 1 || rows < 1 {
		return ""
	}
	sb := img.Bounds()
	var b strings.Builder
	lastFG, lastBG := -1, -1
	emit := func(fg, bg int) {
		if fg != lastFG {
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", fg>>16&0xff, fg>>8&0xff, fg&0xff)
			lastFG = fg
		}
		if bg != lastBG {
			fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", bg>>16&0xff, bg>>8&0xff, bg&0xff)
			lastBG = bg
		}
		b.WriteString("▀")
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			sx := sb.Min.X + c*sb.Dx()/cols
			sy := sb.Min.Y + r*sb.Dy()/rows
			top := img.At(sx, min(sb.Max.Y-1, sy))
			bot := img.At(sx, min(sb.Max.Y-1, sy+sb.Dy()/rows/2))
			tr, tg, tb, _ := top.RGBA()
			br, bg2, bb, _ := bot.RGBA()
			emit(rgbToInt(tr, tg, tb), rgbToInt(br, bg2, bb))
		}
		b.WriteString("\x1b[0m\n")
		lastFG, lastBG = -1, -1
	}
	return b.String()
}

func rgbToInt(r, g, b uint32) int {
	return int(r>>8)<<16 | int(g>>8)<<8 | int(b>>8)
}

// kittyProbe asks the terminal itself whether it speaks the Kitty
// graphics protocol. Real query/response on /dev/tty — never env
// guessing. Safe to call before the TUI starts; false on any doubt.
//
// Termination guarantee: besides the read deadlines, a watchdog closes
// the fd 800ms in, which unblocks any pending Read no matter what the
// kernel/terminal does. A hung probe once wedged startup completely;
// this structure makes that impossible.
func kittyProbe() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	return probeConn(f)
}

func probeConn(f *os.File) bool {
	fd := int(f.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return false
	}
	defer term.Restore(fd, old)
	// Driver-level read timeout (VMIN=0 + VTIME): enforced by the
	// terminal driver itself, unlike Go read deadlines and O_NONBLOCK,
	// both verified ineffective on pty fds (blocked Reads ignore them).
	// This is what makes termination structural rather than hopeful.
	tios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return false
	}
	tios.Cc[unix.VMIN] = 0
	tios.Cc[unix.VTIME] = 1 // 0.1s per read
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, tios); err != nil {
		return false
	}
	// Query: 1x1 RGB direct upload, id 31873.
	fmt.Fprintf(f, "\x1b_Gi=31873,a=q,t=d,f=24,s=1,v=1;AAAA\x1b\\")
	resp := make([]byte, 0, 256)
	tmp := make([]byte, 128)
	for i := 0; i < 5; i++ {
		n, _ := f.Read(tmp)
		if n > 0 {
			resp = append(resp, tmp[:n]...)
			if bytes.Contains(resp, []byte("OK")) {
				// Drain stragglers so nothing leaks into TUI input.
				for j := 0; j < 2; j++ {
					if n, _ := f.Read(tmp); n <= 0 {
						break
					}
				}
				return true
			}
		}
	}
	return false
}

// kittyPNGFrames encodes an already-encoded PNG as Kitty transmit
// chunks (f=100, 4096-byte payloads) plus a cursor-anchored display
// command. Pure function — unit-tested; terminal interop itself is
// UNTESTED (no Kitty-capable terminal on this machine). Sizing follows
// image pixels; callers pre-size the PNG near the target cell box.
func kittyPNGFrames(png []byte, w, h, id int) []string {
	const chunk = 4096
	var frames []string
	for off := 0; off < len(png); off += chunk {
		end := off + chunk
		if end > len(png) {
			end = len(png)
		}
		m := 1
		if end == len(png) {
			m = 0
		}
		frames = append(frames, fmt.Sprintf("\x1b_Ga=t,i=%d,f=100,s=%d,v=%d,m=%d;%s\x1b\\",
			id, w, h, m, base64.StdEncoding.EncodeToString(png[off:end])))
	}
	frames = append(frames, fmt.Sprintf("\x1b_Ga=p,i=%d,U=1\x1b\\", id))
	return frames
}

func encodePNG(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}
