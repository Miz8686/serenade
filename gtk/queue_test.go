package main

import (
	"testing"
)

func TestQueueMove(t *testing.T) {
	q := []string{"/a", "/b", "/c"}
	q, i := queueMove(q, 0, +1)
	if i != 1 || q[0] != "/b" || q[1] != "/a" {
		t.Fatalf("move down: %v %d", q, i)
	}
	q, i = queueMove(q, 0, -1) // boundary: no-op, cursor clamped
	if i != 0 || q[0] != "/b" {
		t.Fatalf("boundary move: %v %d", q, i)
	}
	q, i = queueMove(q, 2, +1)
	if i != 2 || q[2] != "/c" {
		t.Fatalf("tail boundary: %v %d", q, i)
	}
}

func TestQueueRemove(t *testing.T) {
	q := []string{"/a", "/b", "/c"}
	q, i := queueRemove(q, 1)
	if i != 1 || len(q) != 2 || q[1] != "/c" {
		t.Fatalf("remove mid: %v %d", q, i)
	}
	q, i = queueRemove(q, 5) // clamps to tail
	if len(q) != 1 || q[0] != "/a" {
		t.Fatalf("remove clamp: %v %d", q, i)
	}
	q, i = queueRemove(nil, 0)
	if i != 0 || len(q) != 0 {
		t.Fatalf("remove empty: %v %d", q, i)
	}
}

func TestQueueLabel(t *testing.T) {
	if got := queueLabel(Track{Artist: "Al", Title: "T"}); got != "Al – T" {
		t.Fatalf("artist label: %q", got)
	}
	if got := queueLabel(Track{Path: "/x/y.flac", Title: "Solo"}); got != "Solo" {
		t.Fatalf("title fallback: %q", got)
	}
	if got := queueLabel(Track{Path: "/x/y.flac"}); got != "y.flac" {
		t.Fatalf("basename fallback: %q", got)
	}
	if got := trackByPath([]Track{{Path: "/a", Artist: "Al"}}, "/missing").Path; got != "/missing" {
		t.Fatalf("trackByPath passthrough: %q", got)
	}
}
