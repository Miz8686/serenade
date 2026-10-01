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

func TestQueueMeta(t *testing.T) {
	if got := queueMeta(Track{Artist: "Al", Album: "One"}); got != "Al – One" {
		t.Fatalf("full meta: %q", got)
	}
	if got := queueMeta(Track{Album: "Solo"}); got != "Solo" {
		t.Fatalf("album only: %q", got)
	}
	if got := queueMeta(Track{Artist: "Al"}); got != "Al" {
		t.Fatalf("artist only: %q", got)
	}
	if got := queueMeta(Track{Path: "/x/y.flac"}); got != "y.flac" {
		t.Fatalf("basename fallback: %q", got)
	}
}

func TestQueuePathOps(t *testing.T) {
	a := &app{tracks: []Track{{Path: "/a"}, {Path: "/b"}}}
	a.enqueuePath("/a")
	a.enqueuePath("")
	if len(a.queue) != 1 || a.queue[0] != "/a" {
		t.Fatalf("enqueue: %v", a.queue)
	}
	a.playNextPath("/b")
	if len(a.queue) != 2 || a.queue[0] != "/b" || a.queue[1] != "/a" {
		t.Fatalf("play-next prepends: %v", a.queue)
	}
}
