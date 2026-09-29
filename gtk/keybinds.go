package main

// keybinds.go — window-level keys matching the TUI's bindings
// (config-driven via cfg.keyIs, same key names). GDK keyvals are
// normalized to TUI key names; typing inside the search field is
// never hijacked except Escape (clear+unfocus) and Enter (play).

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// normalizeKey maps a GDK keyval name ("a", "space", "Return",
// "Left", "slash"...) to the TUI key namespace ("a", " ", "enter",
// "left", "/"). Single-character names keep their case: the TUI
// distinguishes "a" (queue add) from "A" (queue view) and "g"
// from "G", so lowercasing them would merge distinct bindings.
// Named keys (Return, Escape, Left...) match case-insensitively.
func normalizeKey(keyval uint) string {
	name := gdk.KeyvalName(keyval)
	switch strings.ToLower(name) {
	case "space":
		return " "
	case "return", "kp_enter", "iso_enter":
		return "enter"
	case "escape":
		return "esc"
	}
	if len([]rune(name)) == 1 {
		return name
	}
	return strings.ToLower(name)
}

func (a *app) attachKeys() {
	kc := gtk.NewEventControllerKey()
	kc.ConnectKeyPressed(func(keyval, _ uint, _ gdk.ModifierType) bool {
		return a.onKey(keyval)
	})
	a.win.AddController(kc)
}

func (a *app) onKey(keyval uint) bool {
	k := normalizeKey(keyval)
	is := a.cfg.keyIs
	if a.search != nil && a.search.HasFocus() {
		switch {
		case k == "esc":
			a.search.SetText("")
			a.list.GrabFocus()
			return true
		case is("play", k):
			a.playSelected()
			return true
		}
		return false
	}
	switch {
	case is("toggle", k):
		_ = a.be.toggle()
		return true
	case is("next", k):
		a.playNext()
		return true
	case is("prev", k):
		a.playPrev()
		return true
	case is("seekback", k):
		_ = a.be.seek(-5)
		return true
	case is("seekfwd", k):
		_ = a.be.seek(5)
		return true
	case is("play", k):
		a.playSelected()
		return true
	case is("search", k):
		if a.search != nil {
			a.search.GrabFocus()
		}
		return true
	}
	return false
}

// playSelected plays the selected track row, if any.
func (a *app) playSelected() {
	if a.sel == nil {
		return
	}
	pos := int(a.sel.Selected())
	if pos < 0 || pos >= len(a.rows) || a.rows[pos].kind != rowTrack {
		return
	}
	a.playAt(a.rows[pos].idx)
}
