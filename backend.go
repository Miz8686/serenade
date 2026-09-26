package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/creack/pty"
)

// backend wraps a headless cmus instance. All playback goes through
// cmus-remote against a dedicated socket; this process never decodes audio.
//
// Headless caveat (flagged, not hidden): cmus is an ncurses program. It
// starts and serves the socket without a visible terminal in practice,
// but ANY startup error wedges it behind a "press enter" prompt. The
// supervisor below holds a stdin pipe precisely so it can dismiss that
// prompt. If this ever proves flaky in daily use, the documented
// fallback is mpv + JSON-IPC — not another silent workaround.
type backend struct {
	sock   string
	pty    *os.File // master side; held open so cmus keeps its terminal
	cmd    *exec.Cmd
	log    *os.File
	broken string // non-empty when the backend failed to come up
}

func socketPath() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "serenade.sock")
	}
	return "/tmp/serenade.sock"
}

// runRemote executes: cmus-remote --server <sock> <args...>
func (b *backend) runRemote(args ...string) (string, error) {
	full := append([]string{"--server", b.sock}, args...)
	cmd := exec.Command("cmus-remote", full...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("cmus-remote %v: %v (%s)", args, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// alive reports whether the socket answers a status query.
// Uses a context timeout rather than a Kill race: killing a process
// while Run/Start is still setting it up races inside os/exec
// (caught by -race). CommandContext cancels race-free.
func (b *backend) alive() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "cmus-remote", "--server", b.sock, "-Q")
	cmd.Stdout, cmd.Stderr = nil, nil
	return cmd.Run() == nil
}

// ensureBackend reuses a live backend or supervises a new one.
func ensureBackend() (*backend, error) {
	sock := socketPath()
	b := &backend{sock: sock}
	if b.alive() {
		return b, nil
	}
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".cache", "serenade"), 0o755); err != nil {
		b.broken = err.Error()
		return b, err
	}
	log, err := os.OpenFile(filepath.Join(os.Getenv("HOME"), ".cache", "serenade", "cmus.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		b.broken = err.Error()
		return b, err
	}
	b.log = log
	b.cmd = exec.Command("cmus", "--listen", sock)
	// NOTE (verified): the backend lives only as long as this app.
	// cmus is an ncurses program; when the pty master closes at quit,
	// its terminal I/O fails and it exits even with SIGHUP ignored
	// (tried). So quit means stop-then-quit (see stop()), and a fresh
	// backend spawns on next launch (~1s). No orphan daemons by design.
	// cmus is an ncurses program: without a terminal it wedges or
	// crashes (observed: prompt-block then realloc crash). Give it a
	// private pty whose output goes to the log; the socket is the only
	// interface this app uses.
	ptmx, err := pty.StartWithSize(b.cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		b.broken = err.Error()
		return b, err
	}
	b.pty = ptmx
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_, _ = log.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	// Wait for the socket, then dismiss a possible startup prompt once
	// by sending Enter to its terminal. The alive re-check guards the
	// race where cmus came up healthy between polls — a stray Enter
	// into a live UI would otherwise start playback.
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		if b.alive() {
			return b, nil
		}
	}
	if !b.alive() {
		fmt.Fprintln(ptmx, "") // dismiss "press enter" prompt if wedged
	}
	for i := 0; i < 6; i++ {
		time.Sleep(500 * time.Millisecond)
		if b.alive() {
			return b, nil
		}
	}
	b.cleanup()
	b.broken = "cmus backend unresponsive; log at ~/.cache/serenade/cmus.log"
	return b, fmt.Errorf("backend unresponsive after supervisor dismissal")
}

// cleanup kills a spawned backend (if any) and removes its socket file
// so a later launch never talks to a stale socket.
func (b *backend) cleanup() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	if b.pty != nil {
		_ = b.pty.Close()
	}
	if b.sock != "" {
		_ = os.Remove(b.sock)
	}
}

// status queries current playback state.
func (b *backend) status() (Status, error) {
	var st Status
	if b == nil {
		return st, fmt.Errorf("no backend")
	}
	if b.broken != "" {
		return st, fmt.Errorf("%s", b.broken)
	}
	out, err := b.runRemote("-Q")
	if err != nil {
		return st, err
	}
	st.Raw = out
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "status "):
			st.State = strings.TrimSpace(strings.TrimPrefix(line, "status "))
		case strings.HasPrefix(line, "file "):
			st.File = strings.TrimSpace(strings.TrimPrefix(line, "file "))
		case strings.HasPrefix(line, "duration "):
			fmt.Sscanf(line, "duration %d", &st.Duration)
		case strings.HasPrefix(line, "position "):
			fmt.Sscanf(line, "position %d", &st.Position)
		case strings.HasPrefix(line, "tag artist "):
			st.Artist = strings.TrimSpace(strings.TrimPrefix(line, "tag artist "))
		case strings.HasPrefix(line, "tag title "):
			st.Title = strings.TrimSpace(strings.TrimPrefix(line, "tag title "))
		case strings.HasPrefix(line, "tag album "):
			st.Album = strings.TrimSpace(strings.TrimPrefix(line, "tag album "))
		}
	}
	if st.Title == "" && st.File != "" {
		st.Title = baseName(st.File)
	}
	return st, nil
}

// Status is a parsed `cmus-remote -Q` snapshot.
type Status struct {
	Raw      string
	State    string // playing|paused|stopped
	File     string
	Artist   string
	Title    string
	Album    string
	Duration int
	Position int
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Playback verbs. Errors are returned for the UI to surface.
func (b *backend) playFile(path string) error { _, err := b.runRemote("-f", path); return err }
func (b *backend) toggle() error {
	_, err := b.runRemote("-C", "player-pause")
	return err
}

// stop halts playback. Called on quit: the backend cannot outlive the
// UI (its terminal dies with it), so quit means stop-then-quit.
func (b *backend) stop() {
	if b == nil {
		return
	}
	_, _ = b.runRemote("-C", "player-stop")
}
func (b *backend) next() error { _, err := b.runRemote("-C", "player-next"); return err }
func (b *backend) prev() error { _, err := b.runRemote("-C", "player-prev"); return err }
func (b *backend) seek(delta int) error {
	_, err := b.runRemote("-C", fmt.Sprintf("seek %+d", delta))
	return err
}
func (b *backend) volume(delta string) error {
	_, err := b.runRemote("-C", "vol "+delta)
	return err
}
