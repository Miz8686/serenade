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
- Fixes round (done): art box border dropped so the cover emerges
  from its own backdrop; album subtitles collapse to one per
  contiguous run; title==album singles suppress the echo; accent
  ceiling re-locked to selection-only (transport/seek stay brass).
- Search (done): `GtkSearchEntry` live-filters through the same
  `sahilm/fuzzy` engine as the TUI's `/`; clear restores all.
- Keybinds (done): window-level `GtkEventControllerKey`,
  config-driven via `cfg.keyIs`; case-preserving normalize keeps
  `a`/`A` (and `g`/`G`) distinct like the TUI.
- Queue + shuffle (done): `queue.go` ports `queueMove` /
  `queueRemove` verbatim; shuffle stays a bag — the "Shuffle play"
  button starts a fresh session now, the `s` key/toggle flips mode
  only (no autostart, unlike the TUI quirk). Dialog: live ListBox
  with Up/Down/Remove.
- Lyrics (done): pipeline (`lyrics_pipe.go`) ported as-is —
  embedded → cache+tombstones → one async lrclib fetch. Six-line
  box under the album, synced follow, fixed-token highlight.
  Terminal cell-width helpers deliberately not ported: Pango
  shapes non-Latin scripts natively, so the TUI's Devanagari bug
  class has no equivalent (pinned by `TestLyricsDevanagari`).
- Animation (done, last): track-change crossfade on the art +
  backdrop swap (~150ms out, ~180ms in, 16ms steps). A generation
  counter invalidates stale fades — rapid skips can never land an
  old texture or stick mid-fade (pinned by `TestFadeGen` + a
  `ASER_DEMOSEQ` rapid-cycle capture). Viz paint path untouched.

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
