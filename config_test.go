package main

import (
	"os"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	t.Cleanup(func() { os.Setenv("HOME", "/home/miz") })
	os.Setenv("HOME", "/tmp/nonexistent-home-xyz")
	c := loadConfig()
	if c.Theme.Accent != "#D9A44C" || !c.keyIs("quit", "q") || !c.keyIs("play", "enter") {
		t.Fatalf("defaults wrong: %+v", c.Theme)
	}
}

func TestConfigFileMerge(t *testing.T) {
	t.Cleanup(func() { os.Setenv("HOME", "/home/miz") })
	dir := t.TempDir()
	os.Setenv("HOME", dir)
	os.MkdirAll(dir+"/.config/serenade", 0o755)
	os.WriteFile(dir+"/.config/serenade/config.toml", []byte("[theme]\nbase = \"mono\"\naccent = \"#FF0000\"\n[keys]\nquit = [\"ctrl+q\"]\n"), 0o644)
	c := loadConfig()
	if c.Theme.Accent != "#FF0000" || c.Theme.Text != "#FFFFFF" {
		t.Fatalf("theme merge wrong: %+v", c.Theme)
	}
	if !c.keyIs("quit", "ctrl+q") || c.keyIs("quit", "q") {
		t.Fatalf("key merge wrong: %v", c.Keys.Quit)
	}
	if !c.keyIs("play", "enter") {
		t.Fatalf("untouched keys must keep defaults")
	}
}
