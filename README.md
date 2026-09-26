# Serenade

Minimal terminal music player in Go — Bubble Tea + Bubbles + Lip Gloss.
One self-contained binary, no daemons, no shell-script glue.

![Arch](https://img.shields.io/badge/Arch_Linux-1793d1?style=for-the-badge&logo=archlinux&logoColor=white)
![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)

## What it is

- **Library**: self-indexed FLAC/MP3/M4A/OGG tags, sorted artist → album → track
- **Playback**: headless `cmus` backend over its socket protocol (supervised,
  auto-spawned). Quitting the UI stops playback — no orphan daemons
- **Cover art**: embedded art + folder fallback, native half-block terminal
  renderer, per-track accent colors sampled from the art
- **Visualizer**: native FFT loop (Hann window, log buckets, peak-hold)
  tapping the PipeWire monitor via `pw-cat` — no cava needed
- **Bar**: gradient progress, MPRIS exposed by cmus itself (Waybar-ready)
- **Extras**: fuzzy search, in-memory queue, `?` help overlay,
  `~/.config/serenade/config.toml` for theme + keybinds

## Requirements

- `cmus`, `pw-cat` + `pactl` (visualizer only), a Nerd Font, truecolor terminal
- Audio plays through whatever PipeWire default sink is set

## Build & install

```bash
git clone https://github.com/Miz8686/serenade.git
cd serenade
go build -o serenade .
cp serenade ~/.local/bin/
```

No config needed — zero-config works out of the box.

## Usage

```
serenade            # library + player
serenade --selftest # backend, index, status, render, mouse checks
```

| Key | Action |
|---|---|
| `Enter` | play selected |
| `space` | play / pause |
| `n` / `p` | next / previous |
| `h` / `l` | seek ∓5s |
| `+` / `-` | volume |
| `/` | fuzzy search (Enter plays, Esc clears) |
| `a` | add to queue (auto-advance drains it first) |
| `Tab` | focus library / detail |
| `?` | keybind help overlay |
| `q` | quit (stops playback) |
| mouse | click select, double-click play, wheel scroll |

All rebindable in `config.toml`. Playback keys work from either pane.

## Config

`~/.config/serenade/config.toml` (optional):

```toml
[theme]
base = "mocha"   # mocha | mono (high-contrast)
# accent = "#CBA6F7"   # any token overridable; per-track art accent wins

[keys]
quit = ["q", "ctrl+c"]
# any action in ? overlay rebindable the same way
```

## Layout

```
┌─ library ─────────────┬─ art ─────────┐
│ artist – title         │ Now Playing   │
│ ...                    │ ─── viz ───   │
├─ ▶ progress ──────────┴───────────────┤
```

## Notes

- The visualizer pauses when the terminal loses focus or nothing plays.
- `pw-cat --target` takes a sink **serial** (resolved via `pw-dump`),
  not a monitor name — monitor names silently fall back to the default
  source. This is handled internally; just don't "simplify" it back.
- cmus needs a terminal to stay alive, so the backend runs in a private
  pty owned by the app. If it ever misbehaves, mpv + JSON-IPC is the
  documented fallback, not more glue.
