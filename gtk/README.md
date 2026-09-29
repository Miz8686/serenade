# a-serenade-gtk

Serenade's backend brain in a GTK4 body (gotk4). Phase 1.

## Reuse, not rebuild

Ported verbatim from the parent directory (plain Go, zero
toolkit dependency): `backend.go` (headless cmus pty-supervisor),
`library.go` (tag indexer), `config.go` (theme), `art.go`
(cover-art cache + accent extraction), plus the pure transport
decisions in `player.go` (`pickNext`, shuffle bag, prev/next path,
natural-end). Only the view layer (`main.go`, `listview.go`,
`nowplaying.go`, `style.css`) is new. Shares the TUI's backend
socket, config, and art cache.

## Phase 1 scope (stop here)

- Library ListView (artist headings as non-selectable rows) +
  Now Playing panel (GdkTexture art, type hierarchy) wired to real
  playback: select, play, pause, next/prev, seek.
- Per-track accent drives the list selection state ONLY
  (see `applyAccent` + the ceiling comment in `style.css`).
- Phase 2 (done): Cairo spectrum strip on the ported
  PipeWire/FFT tap — plays only while playing+focused, idle
  baseline otherwise, tap strictly restarted per track.
- Phase 3 (done): full-window blurred-art backdrop (GtkOverlay,
  scrim baked in Go with the TUI's adaptive math, one texture per
  track) + transparent panes. Contrast locked per cover.
- Out of scope: lyrics, animation, shuffle UI, queue UI, keybinds.

## Deviations from the brief (flagged, not hidden)

- `GtkListView` header factories only bind position 0 without a
  section model (verified live: a single heading up top). Real
  sections would need GObject subclassing in Go — out of scope.
  Headings are rows in the model instead (same liner-notes
  structure as the TUI), non-selectable, full Pango hierarchy.
- gotk4 README caveat check: "memory leaks and sometimes crashes
  may occur in certain parts of the API". GdkTexture path
  smoke-tested clean (`NewTextureFromBytes` + `Picture`, real
  384×384 art, no crash); no other exotic API used. Nothing hit.

## Run

```
go build -o aser .
./aser   # shares serenade's backend socket + config + art cache
```

Screenshots headless: `WLR_BACKENDS=headless sway` + `grim`.
`ASER_DEMO=/path/file.flac` fabricates a playing state for
screenshots where no audio device exists; never touches playback.
