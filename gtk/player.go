package main

// player.go — transport decisions ported pure from serenade's TUI.
// No view code, no toolkit dependency: queue precedence, shuffle
// bag, library order. The GTK layer calls these and issues backend
// commands itself.

import "math/rand"

// prevTrackPath mirrors nextTrackPath backwards, wrapping to the last
// track. Previous ALWAYS means previous-in-library — no restart
// threshold, no backend playlist involved.
func prevTrackPath(tracks []Track, curFile string) (string, bool) {
	if len(tracks) == 0 {
		return "", false
	}
	for i, t := range tracks {
		if t.Path == curFile {
			return tracks[(i-1+len(tracks))%len(tracks)].Path, true
		}
	}
	return tracks[len(tracks)-1].Path, true
}

// nextTrackPath returns the path after prevFile in library order,
// wrapping to the first track. Pure (no backend) for testability.
func nextTrackPath(tracks []Track, prevFile string) (string, bool) {
	for i, t := range tracks {
		if t.Path == prevFile {
			if len(tracks) == 0 {
				return "", false
			}
			return tracks[(i+1)%len(tracks)].Path, true
		}
	}
	return "", false
}

// shuffleState is a shuffle-bag: Fisher-Yates order walked once,
// reshuffled on exhaustion. Independent random picks cluster and
// repeat; a bag guarantees every track plays before any repeats.
type shuffleState struct {
	on    bool
	order []int
	pos   int
}

// freshBag builds a shuffled index order. When reshuffling, the first
// element avoids repeating the just-played tail track when possible.
func freshBag(n int, avoidIdx int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := randInt(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	if n > 1 && order[0] == avoidIdx {
		order[0], order[1] = order[1], order[0]
	}
	return order
}

// randInt returns [0,n) using the auto-seeded global source.
func randInt(n int) int {
	if n <= 1 {
		return 0
	}
	return rand.Intn(n)
}

// pickNext is the single "what plays next" decision, in precedence
// order: explicit queue head, shuffle bag, library order.
func pickNext(queue []string, tracks []Track, prevFile string, shuf *shuffleState) (next string, rest []string, ok bool) {
	if len(queue) > 0 {
		return queue[0], queue[1:], true
	}
	if shuf != nil && shuf.on {
		if len(shuf.order) != len(tracks) {
			shuf.order = freshBag(len(tracks), -1)
			shuf.pos = 0
		}
		if len(shuf.order) == 0 {
			return "", queue, false
		}
		if shuf.pos >= len(shuf.order) {
			prevIdx := -1
			for i, t := range tracks {
				if t.Path == prevFile {
					prevIdx = i
				}
			}
			shuf.order = freshBag(len(tracks), prevIdx)
			shuf.pos = 0
		}
		idx := shuf.order[shuf.pos]
		shuf.pos++
		if idx < 0 || idx >= len(tracks) {
			return "", queue, false
		}
		return tracks[idx].Path, queue, true
	}
	next, ok = nextTrackPath(tracks, prevFile)
	return next, queue, ok
}

// isNaturalEnd reports a genuine track finish: was playing a known
// file, now stopped, and the file either stayed put or was cleared.
// Same dormant-ambiguity note as the TUI: no mid-session stop exists
// here either (quit-only), so this path means finish or external -s.
func isNaturalEnd(prev, cur Status) bool {
	return prev.State == "playing" && prev.File != "" && cur.State == "stopped" &&
		(cur.File == prev.File || cur.File == "")
}
