package main

// config.go — ~/.config/serenade/config.toml.
//
// Theme colors and keybinds, both overridable. Zero-config works exactly
// like the old hardcoded defaults. This same effective map feeds the
// help overlay, so docs can never drift from behavior.

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Theme holds semantic color tokens. Empty string = fall back to base.
type Theme struct {
	Base string `toml:"base"`
	// background_art swaps the sharp art box for blurred art behind
	// the Now Playing text. Default off until seen and chosen.
	BackgroundArt bool   `toml:"background_art"`
	Bg            string `toml:"bg"`
	Surface       string `toml:"surface"`
	Dim           string `toml:"dim"`
	Text          string `toml:"text"`
	Muted         string `toml:"muted"`
	Accent        string `toml:"accent"`
	Accent2       string `toml:"accent2"`
	Success       string `toml:"success"`
	Warn          string `toml:"warn"`
	Error         string `toml:"error"`
}

// Keys maps actions to one or more key names as reported by
// tea.KeyMsg.String() (e.g. "enter", " ", "ctrl+c", "pgup").
type Keys struct {
	Up        []string `toml:"up"`
	Down      []string `toml:"down"`
	PageUp    []string `toml:"page_up"`
	PageDown  []string `toml:"page_down"`
	Home      []string `toml:"home"`
	End       []string `toml:"end"`
	Tab       []string `toml:"tab"`
	Quit      []string `toml:"quit"`
	Play      []string `toml:"play"`
	Toggle    []string `toml:"toggle"`
	Next      []string `toml:"next"`
	Prev      []string `toml:"prev"`
	SeekBack  []string `toml:"seek_back"`
	SeekFwd   []string `toml:"seek_fwd"`
	VolUp     []string `toml:"vol_up"`
	VolDown   []string `toml:"vol_down"`
	Search    []string `toml:"search"`
	Shuffle   []string `toml:"shuffle"`
	Queue     []string `toml:"queue"`
	QueueView []string `toml:"queue_view"`
	Help      []string `toml:"help"`
}

// Config is the effective (base palette + overrides, default keys +
// overrides) configuration.
type Config struct {
	Theme Theme `toml:"theme"`
	Keys  Keys  `toml:"keys"`
}

var mochaTheme = Theme{
	Base: "mocha", Bg: "#1E1E2E", Surface: "#313244", Dim: "#181825",
	Text: "#CDD6F4", Muted: "#9399B2", Accent: "#CBA6F7", Accent2: "#89B4FA",
	Success: "#A6E3A1", Warn: "#F9E2AF", Error: "#F38BA8",
}

var monoTheme = Theme{
	Base: "mono", Bg: "#000000", Surface: "#1A1A1A", Dim: "#0D0D0D",
	Text: "#FFFFFF", Muted: "#B0B0B0", Accent: "#FFD700", Accent2: "#00E5FF",
	Success: "#00FF87", Warn: "#FFAF00", Error: "#FF5252",
}

func defaultKeys() Keys {
	return Keys{
		Up: []string{"up", "k"}, Down: []string{"down", "j"},
		PageUp: []string{"pgup"}, PageDown: []string{"pgdown"},
		Home: []string{"home", "g"}, End: []string{"end", "G"},
		Tab: []string{"tab"}, Quit: []string{"q", "ctrl+c"},
		Play: []string{"enter"}, Toggle: []string{" "},
		Next: []string{"n"}, Prev: []string{"p"},
		SeekBack: []string{"left", "h"}, SeekFwd: []string{"right", "l"},
		VolUp: []string{"+", "="}, VolDown: []string{"-", "_"},
		Search: []string{"/"}, Shuffle: []string{"s"}, Queue: []string{"a"},
		QueueView: []string{"A"},
		Help:      []string{"?"},
	}
}

func defaultConfig() Config {
	return Config{Theme: mochaTheme, Keys: defaultKeys()}
}

// keySets returns action names in stable display order with their
// effective bindings. Single source for the help overlay.
func (c Config) keySets() []keySet {
	k := c.Keys
	return []keySet{
		{"Navigate", append(append([]string{}, k.Up...), k.Down...)},
		{"Page jump", append(append([]string{}, k.PageUp...), k.PageDown...)},
		{"First / last", append(append([]string{}, k.Home...), k.End...)},
		{"Play selected", k.Play},
		{"Play / pause", k.Toggle},
		{"Next / prev track", append(append([]string{}, k.Next...), k.Prev...)},
		{"Seek ∓5s", append(append([]string{}, k.SeekBack...), k.SeekFwd...)},
		{"Volume", append(append([]string{}, k.VolUp...), k.VolDown...)},
		{"Fuzzy search", k.Search},
		{"Shuffle", k.Shuffle},
		{"Queue add", k.Queue},
		{"Queue view", k.QueueView},
		{"Focus pane", k.Tab},
		{"Help", k.Help},
		{"Quit", k.Quit},
	}
}

type keySet struct {
	action string
	keys   []string
}

func (c Config) keyIs(action, key string) bool {
	var set []string
	switch action {
	case "up":
		set = c.Keys.Up
	case "down":
		set = c.Keys.Down
	case "pageup":
		set = c.Keys.PageUp
	case "pagedown":
		set = c.Keys.PageDown
	case "home":
		set = c.Keys.Home
	case "end":
		set = c.Keys.End
	case "tab":
		set = c.Keys.Tab
	case "quit":
		set = c.Keys.Quit
	case "play":
		set = c.Keys.Play
	case "toggle":
		set = c.Keys.Toggle
	case "next":
		set = c.Keys.Next
	case "prev":
		set = c.Keys.Prev
	case "seekback":
		set = c.Keys.SeekBack
	case "seekfwd":
		set = c.Keys.SeekFwd
	case "volup":
		set = c.Keys.VolUp
	case "voldown":
		set = c.Keys.VolDown
	case "search":
		set = c.Keys.Search
	case "shuffle":
		set = c.Keys.Shuffle
	case "queue":
		set = c.Keys.Queue
	case "queueview":
		set = c.Keys.QueueView
	case "help":
		set = c.Keys.Help
	}
	for _, k := range set {
		if k == key {
			return true
		}
	}
	return false
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "serenade", "config.toml")
}

// loadConfig returns effective config: built-in defaults, overlaid with
// ~/.config/serenade/config.toml when present. Missing file is fine.
func loadConfig() Config {
	c := defaultConfig()
	path := configPath()
	var file struct {
		Theme Theme `toml:"theme"`
		Keys  Keys  `toml:"keys"`
	}
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return c
	}
	base := mochaTheme
	if file.Theme.Base == "mono" {
		base = monoTheme
	}
	t := base
	f := file.Theme
	if f.Bg != "" {
		t.Bg = f.Bg
	}
	if f.Surface != "" {
		t.Surface = f.Surface
	}
	if f.Dim != "" {
		t.Dim = f.Dim
	}
	if f.Text != "" {
		t.Text = f.Text
	}
	if f.Muted != "" {
		t.Muted = f.Muted
	}
	if f.Accent != "" {
		t.Accent = f.Accent
	}
	if f.Accent2 != "" {
		t.Accent2 = f.Accent2
	}
	if f.Success != "" {
		t.Success = f.Success
	}
	if f.Warn != "" {
		t.Warn = f.Warn
	}
	if f.Error != "" {
		t.Error = f.Error
	}
	c.Theme = t
	merge := func(dst *[]string, src []string) {
		if len(src) > 0 {
			*dst = src
		}
	}
	k, fk := &c.Keys, file.Keys
	merge(&k.Up, fk.Up)
	merge(&k.Down, fk.Down)
	merge(&k.PageUp, fk.PageUp)
	merge(&k.PageDown, fk.PageDown)
	merge(&k.Home, fk.Home)
	merge(&k.End, fk.End)
	merge(&k.Tab, fk.Tab)
	merge(&k.Quit, fk.Quit)
	merge(&k.Play, fk.Play)
	merge(&k.Toggle, fk.Toggle)
	merge(&k.Next, fk.Next)
	merge(&k.Prev, fk.Prev)
	merge(&k.SeekBack, fk.SeekBack)
	merge(&k.SeekFwd, fk.SeekFwd)
	merge(&k.VolUp, fk.VolUp)
	merge(&k.VolDown, fk.VolDown)
	merge(&k.Search, fk.Search)
	merge(&k.Shuffle, fk.Shuffle)
	merge(&k.Queue, fk.Queue)
	merge(&k.Help, fk.Help)
	return c
}
