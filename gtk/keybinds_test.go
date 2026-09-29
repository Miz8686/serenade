package main

import (
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

func TestNormalizeKey(t *testing.T) {
	cases := map[uint]string{
		gdk.KEY_a:      "a",
		gdk.KEY_A:      "A", // queue view stays distinct from queue add
		gdk.KEY_space:  " ",
		gdk.KEY_Return: "enter",
		gdk.KEY_Escape: "esc",
		gdk.KEY_Left:   "left",
		gdk.KEY_slash:  "slash",
	}
	for kv, want := range cases {
		if got := normalizeKey(kv); got != want {
			t.Fatalf("keyval %d: got %q want %q", kv, got, want)
		}
	}
}
