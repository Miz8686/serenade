package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
	"golang.org/x/image/draw"
)

// House palette defaults (mirrors mochaTheme in config.go until
// buildBaseStyles runs): brass & verdigris on warm ink.
var (
	colBG      = lipgloss.Color("#161310")
	colSurface = lipgloss.Color("#2E2822")
	colText    = lipgloss.Color("#EDE0C8")
	colMuted   = lipgloss.Color("#9C8D75")
	colAccent  = lipgloss.Color("#D9A44C")
	colAccent2 = lipgloss.Color("#6FA598")
	colError   = lipgloss.Color("#F38BA8")

	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent)
	styleBlurBorder    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colSurface)
	// One-frame system: heavy outer edge + quiet internal
	// dividers. Weight carries hierarchy; color stays out of the way.
	styleOuterFrame = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(colMuted)
	styleDivider    = lipgloss.NewStyle().Foreground(colSurface)
	styleSelected   = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#161310")).Bold(true)
	stylePlaying    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHL         = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleMuted      = lipgloss.NewStyle().Foreground(colMuted)
	styleTitle      = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleArtist     = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
	styleError      = lipgloss.NewStyle().Foreground(colError)
)

type focusPane int

const (
	focusList focusPane = iota
	focusDetail
)

type model struct {
	be        *backend
	cfg       Config
	tracks    []Track
	indexing  bool
	indexErr  string
	cursor    int
	offset    int
	view      []Track  // displayed rows: full library or live fuzzy filter
	queue     []string // in-memory play queue (paths); auto-advance drains it first
	viewHL    [][]int  // per-row matched char indexes (fuzzy highlight)
	searching bool
	showHelp  bool // ? overlay, generated from cfg.keySets()
	showQueue bool // queue overlay: view/reorder/remove the live queue
	qCursor   int  // selection within the queue overlay
	shuf      shuffleState
	searchBox textinput.Model
	focus     focusPane
	width     int
	height    int
	status    Status
	prev      Status // previous poll; detects natural track end
	beErr     string
	// Phase-2 art state. artImg holds cached pixels (never re-decoded);
	// artBlock is the pre-rendered right-pane art section.
	artImg    image.Image
	artPrim   string
	artSec    string
	artBlock  string
	artFile   string
	bgImg     image.Image // blurred+scrimmed variant for background mode
	kitty     bool
	kittyID   int
	appliedAc string
	// Transport button flash feedback.
	flash      string
	flashZones []btnZone
	// Phase-3 visualizer state.
	tap       *vizTap
	levels    []float64
	peaks     []float64
	vizActive bool
	vizErr    string
	focused   bool // terminal focus; false pauses background polling
	detail    viewport.Model
	lastClick time.Time
	lastRow   int
}

type indexMsg struct {
	tracks []Track
	err    error
}
type statusMsg struct {
	st  Status
	err error
}

// themeBase is the configured base palette. Per-track accents
// override it via applyAccent; it is the fallback for art-less tracks.
var themeBase = mochaTheme

// buildBaseStyles rebuilds every package style from themeBase.
func buildBaseStyles() {
	t := themeBase
	colBG = lipgloss.Color(t.Bg)
	colSurface = lipgloss.Color(t.Surface)
	colText = lipgloss.Color(t.Text)
	colMuted = lipgloss.Color(t.Muted)
	colAccent = lipgloss.Color(t.Accent)
	colAccent2 = lipgloss.Color(t.Accent2)
	colError = lipgloss.Color(t.Error)
	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent).Background(colBG)
	styleBlurBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colSurface).Background(colBG)
	styleOuterFrame = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(colMuted).Background(colBG)
	styleDivider = lipgloss.NewStyle().Foreground(colSurface)
	styleSelected = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#161310")).Bold(true)
	stylePlaying = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHL = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleMuted = lipgloss.NewStyle().Foreground(colMuted)
	styleTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleArtist = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
	styleError = lipgloss.NewStyle().Foreground(colError)
}

func newModel(be *backend, kitty bool, cfg Config) model {
	vp := viewport.New(0, 0)
	themeBase = cfg.Theme
	buildBaseStyles()
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.CharLimit = 64
	return model{
		be:        be,
		cfg:       cfg,
		tap:       &vizTap{},
		searchBox: ti,
		kitty:     kitty,
		indexing:  true,
		focused:   true,
		detail:    vp,
		width:     80,
		height:    24,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			tracks, err := indexLibrary()
			return indexMsg{tracks, err}
		},
		pollBackend(m.be),
	)
}

func pollBackend(be *backend) tea.Cmd {
	return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg {
		st, err := be.status()
		return statusMsg{st, err}
	})
}

func (m model) visibleRows() int {
	// Top area minus the list title line: outer frame (2) + status
	// block (divider, bar, buttons = 3) + title (1).
	return max(1, m.height-2-3-1)
}

// topH is the content height above the status divider.
func (m model) topH() int { return max(1, m.height-2-3) }

func (m *model) clampCursor() {
	n := len(m.view)
	if n == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = min(max(m.cursor, 0), n-1)
	vis := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+vis {
		m.offset = m.cursor - vis + 1
	}
	// Headings and gaps consume screen rows: keep shifting the
	// track-offset anchor until the cursor's own screen row fits.
	for m.screenSpan(m.offset, m.cursor) > vis && m.offset < m.cursor {
		m.offset++
	}
}

// list line kinds: track rows plus liner-notes chrome.
const (
	llTrack = iota
	llArtist
	llGap
)

type listLine struct {
	kind int
	idx  int // view index for llTrack/llArtist
}

func artistKey(a string) string { return strings.ToLower(a) }

// screenSpan counts emitted screen rows from track a through track b
// inclusive (headings + gaps + tracks). Must match listWindow exactly
// or the cursor can slide under the fold.
func (m model) screenSpan(a, b int) int {
	if m.searching || a > b {
		return max(0, b-a+1)
	}
	n, prevA, first := 0, "", true
	for i := a; i <= b && i < len(m.view); i++ {
		ak := artistKey(m.view[i].Artist)
		if first || ak != prevA {
			if !first {
				n++
			}
			n++
			prevA, first = ak, false
		}
		n++
	}
	return n
}

// listWindow builds the visible screen lines from the track offset:
// artist headings with breathing room (liner notes), quiet title rows
// beneath. Searching falls back to a flat fuzzy list so highlight
// indexes keep aligning one row per track.
func (m model) listWindow() []listLine {
	vis := m.visibleRows()
	var lines []listLine
	if m.searching {
		for i := m.offset; i < m.offset+vis && i < len(m.view); i++ {
			lines = append(lines, listLine{llTrack, i})
		}
		return lines
	}
	prevA, first := "", true
	for i := m.offset; i < len(m.view) && len(lines) < vis; i++ {
		ak := artistKey(m.view[i].Artist)
		if first || ak != prevA {
			need := 2 // heading + its first track
			if !first {
				need = 3 // gap + heading + track
			}
			if len(lines)+need > vis {
				break // never orphan a heading at the fold
			}
			if !first {
				lines = append(lines, listLine{llGap, -1})
			}
			lines = append(lines, listLine{llArtist, i})
			prevA, first = ak, false
		}
		if len(lines)+1 > vis {
			break
		}
		lines = append(lines, listLine{llTrack, i})
	}
	return lines
}

// followPlaying jumps scroll offset AND cursor to the playing track's
// row. Fires only on track-start transitions (see statusMsg), never on
// continuous position polls. Accepted trade-off: takes over the cursor
// if the user is mid-browsing exactly when a transition fires — ship
// the simple version first, revisit only if real use shows pain.
func (m *model) followPlaying(path string) {
	if path == "" {
		return
	}
	for i, t := range m.view {
		if t.Path == path {
			m.cursor = i
			vis := m.visibleRows()
			m.offset = min(max(0, len(m.view)-vis), max(0, i-vis/2))
			return
		}
	}
}

func (m model) playingPath() string { return m.status.File }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layoutPanes()
		if m.artImg != nil {
			m.artBlock = m.renderArt()
		} else if m.artFile != "" || m.status.File != "" {
			m.artBlock = m.emptyArt()
		}
		return m, nil

	case indexMsg:
		m.indexing = false
		if msg.err != nil {
			m.indexErr = msg.err.Error()
		} else {
			m.tracks = msg.tracks
			m.view = msg.tracks
		}
		m.clampCursor()
		return m, nil

	case statusMsg:
		if msg.err != nil {
			m.beErr = msg.err.Error()
		} else {
			m.beErr = ""
			if isNaturalEnd(m.prev, msg.st) {
				prevFile := m.prev.File
				m.status = msg.st
				m.prev = msg.st
				m.syncDetail()
				m.advance(prevFile)
				if m.focused {
					return m, tea.Batch(pollBackend(m.be), m.syncViz())
				}
				return m, nil
			}
			m.prev = m.status
			m.status = msg.st
			if m.status.File != m.artFile {
				m.loadArt(m.status.File)
			}
			// Track-start transition (not a position poll): follow the
			// now-playing row so it's visible and selected. Covers
			// auto-advance, shuffle jumps and manual plays alike, since
			// all of them surface here as a file change.
			if m.status.File != m.prev.File {
				m.followPlaying(m.status.File)
			}
		}
		m.syncDetail()
		if m.focused {
			return m, tea.Batch(pollBackend(m.be), m.syncViz())
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, tea.Batch(pollBackend(m.be), m.syncViz())

	case tea.BlurMsg:
		// Terminal unfocused: stop scheduling polls. The in-flight
		// statusMsg that arrives simply won't re-arm the ticker.
		m.focused = false
		return m, m.syncViz()

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case flashMsg:
		m.flash = ""
		return m, nil

	case vizTickMsg:
		if !m.vizActive || !m.shouldViz() {
			m.tap.stop()
			m.vizActive = false
			m.levels, m.peaks = nil, nil
			return m, nil
		}
		// Health-check the tap every frame: a tap that died on its
		// own (not via stop) must surface as an error and deactivate,
		// never spin forever on empty frames with vizActive stuck on.
		if running, broken := m.tap.state(); !running {
			m.tap.stop()
			m.vizActive = false
			m.levels, m.peaks = nil, nil
			if broken != "" {
				m.vizErr = broken
			}
			return m, nil
		}
		if frame := m.tap.frame(vizFFTSize); frame != nil {
			bars := m.vizBars()
			m.levels = fftLevels(frame, vizRate, bars)
			if len(m.peaks) != len(m.levels) {
				m.peaks = make([]float64, len(m.levels))
			}
			for i, l := range m.levels {
				if l > m.peaks[i] {
					m.peaks[i] = l
				} else {
					m.peaks[i] *= vizDecay
				}
			}
		}
		return m, m.vizTick()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// applyFilter rebuilds the displayed view from the query using fuzzy
// matching. Empty query restores the full library.
func (m *model) applyFilter() {
	q := m.searchBox.Value()
	if q == "" {
		m.view = m.tracks
		m.viewHL = nil
		m.clampCursor()
		return
	}
	labels := make([]string, len(m.tracks))
	for i, t := range m.tracks {
		labels[i] = t.label()
	}
	matches := fuzzy.Find(q, labels)
	m.view = make([]Track, 0, len(matches))
	m.viewHL = make([][]int, 0, len(matches))
	for _, mt := range matches {
		m.view = append(m.view, m.tracks[mt.Index])
		m.viewHL = append(m.viewHL, mt.MatchedIndexes)
	}
	m.clampCursor()
}

// fuzzyLine renders label with matched chars in the accent color.
func fuzzyLine(label string, idx []int) string {
	if len(idx) == 0 {
		return label
	}
	hit := map[int]bool{}
	for _, i := range idx {
		hit[i] = true
	}
	var b strings.Builder
	runes := []rune(label)
	for i, r := range runes {
		if hit[i] {
			b.WriteString(styleHL.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	is := m.cfg.keyIs
	// Help overlay is modal: quit still quits, everything else either
	// closes it or is swallowed so keys can't leak through.
	if m.showHelp {
		if is("quit", k) {
			m.be.stop()
			return m, tea.Quit
		}
		if k == "esc" || is("help", k) {
			m.showHelp = false
		}
		return m, nil
	}
	// Queue overlay is modal like help: quit still quits, esc/A
	// closes, navigation + reorder + remove act on the live queue
	// pickNext() drains, everything else is swallowed.
	if m.showQueue {
		if is("quit", k) {
			m.be.stop()
			return m, tea.Quit
		}
		switch {
		case k == "esc" || is("queueview", k):
			m.showQueue = false
		case is("up", k):
			m.qCursor--
			m.clampQCursor()
		case is("down", k):
			m.qCursor++
			m.clampQCursor()
		case k == "K":
			m.queue, m.qCursor = queueMove(m.queue, m.qCursor, -1)
		case k == "J":
			m.queue, m.qCursor = queueMove(m.queue, m.qCursor, +1)
		case k == "d":
			m.queue, m.qCursor = queueRemove(m.queue, m.qCursor)
		}
		return m, nil
	}
	// Search mode captures everything except Enter (play+exit) and
	// Esc (exit, restore full list). Playback keys stay silent here
	// so typing a space doesn't toggle pause mid-query.
	if m.searching {
		switch {
		case k == "enter":
			m.searching = false
			m.searchBox.Blur()
			m.searchBox.SetValue("")
			m.applyFilter()
			m.playCursor()
			return m, nil
		case k == "esc":
			m.searching = false
			m.searchBox.Blur()
			m.searchBox.SetValue("")
			m.applyFilter()
			return m, nil
		}
		var cmd tea.Cmd
		m.searchBox, cmd = m.searchBox.Update(msg)
		m.applyFilter()
		return m, cmd
	}
	if is("search", k) {
		m.searching = true
		m.searchBox.Focus()
		return m, nil
	}
	// Playback controls are global: they work from either pane.
	// Only list navigation, detail scrolling, tab-focus and quit are scoped.
	switch {
	case is("quit", k):
		m.be.stop()
		return m, tea.Quit
	case is("toggle", k):
		_ = m.be.toggle()
		return m, nil
	case is("next", k):
		m.playNext()
		return m, nil
	case is("prev", k):
		m.playPrev()
		return m, nil
	case is("seekback", k):
		_ = m.be.seek(-5)
		return m, nil
	case is("seekfwd", k):
		_ = m.be.seek(5)
		return m, nil
	case is("volup", k):
		_ = m.be.volume("+5%")
		return m, nil
	case is("voldown", k):
		_ = m.be.volume("-5%")
		return m, nil
	case is("play", k):
		if m.focus == focusList {
			m.playCursor()
			return m, nil
		}
	case is("queue", k):
		m.enqueueCursor()
		m.syncDetail()
		return m, nil
	case is("shuffle", k):
		m.toggleShuffle()
		m.syncDetail()
		return m, nil
	}
	// Detail pane gets navigation keys when focused.
	if m.focus == focusDetail {
		switch {
		case is("tab", k):
			m.focus = focusList
			m.layoutPanes()
			return m, nil
		case is("help", k):
			m.showHelp = true
			return m, nil
		case is("queueview", k):
			m.showQueue = true
			m.qCursor = 0
			m.clampQCursor()
			return m, nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	switch {
	case is("tab", k):
		m.focus = focusDetail
		m.layoutPanes()
		return m, nil
	case is("help", k):
		m.showHelp = true
		return m, nil
	case is("queueview", k):
		m.showQueue = true
		m.qCursor = 0
		m.clampQCursor()
		return m, nil
	case is("up", k):
		m.cursor--
		m.clampCursor()
	case is("down", k):
		m.cursor++
		m.clampCursor()
	case is("pgup", k):
		m.cursor -= m.visibleRows()
		m.clampCursor()
	case is("pgdown", k):
		m.cursor += m.visibleRows()
		m.clampCursor()
	case is("home", k):
		m.cursor = 0
		m.clampCursor()
	case is("end", k):
		m.cursor = len(m.tracks) - 1
		m.clampCursor()
	}
	return m, nil
}

// isNaturalEnd reports a genuine track finish: was playing a known
// file, now stopped, and the file either stayed put or was cleared
// (cmus clears it when its library is empty; holds it otherwise).
// A *different* file means external action — never advance. A manual
// next() lands on a different file, so this cannot double-skip input.
//
// KNOWN AMBIGUITY (dormant, not a live bug): a polled playing→stopped
// same-file transition is INDISTINGUISHABLE from an external
// `cmus-remote -s`. This is safe only because nothing in our UI issues
// a mid-session stop — be.stop() is quit-only, transport is toggle /
// next / prev / play. If a real stop feature is ever added, it must
// disambiguate (e.g. an intent flag set around the command), or every
// user stop will instantly auto-advance. TestNaturalEndAdvances locks
// the current behavior so that day trips a test instead of shipping
// silently.
func isNaturalEnd(prev, cur Status) bool {
	return prev.State == "playing" && prev.File != "" && cur.State == "stopped" &&
		(cur.File == prev.File || cur.File == "")
}

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

// playNext/playPrev implement transport through OUR library order
// (queue-aware for next), never cmus's internal playlist. cmus's
// player-next/prev on its single-file -f playlist either no-ops or
// restarts the current track — both reported as bugs.
func (m *model) playNext() {
	prev := m.status.File
	if prev == "" && len(m.queue) == 0 && len(m.tracks) > 0 {
		// Idle with a library: start from the top instead of
		// silently no-op'ing (same "hit it and it just works"
		// principle as shuffle-on-idle).
		if err := m.be.playFile(m.tracks[0].Path); err != nil {
			m.beErr = err.Error()
		}
		return
	}
	next, rest, ok := pickNext(m.queue, m.tracks, prev, &m.shuf)
	if !ok {
		return
	}
	m.queue = rest
	if err := m.be.playFile(next); err != nil {
		m.beErr = err.Error()
	}
}

func (m *model) playPrev() {
	// Note: prevTrackPath falls back to the last track on unknown
	// current file, so prev-from-idle starts at the bottom with no
	// special-casing needed here.
	prev, ok := prevTrackPath(m.tracks, m.status.File)
	if !ok {
		return
	}
	if err := m.be.playFile(prev); err != nil {
		m.beErr = err.Error()
	}
}

func (m *model) advance(prevFile string) {
	next, rest, ok := pickNext(m.queue, m.tracks, prevFile, &m.shuf)
	if !ok {
		return
	}
	m.queue = rest
	if err := m.be.playFile(next); err != nil {
		m.beErr = err.Error()
	}
}

// enqueueCursor appends the cursor's track (current filter view) to
// the play queue.
// reshuffleAndPlay is the shuffle BUTTON behavior (distinct from the
// `s` key toggle): enable shuffle, generate a fresh full-library bag,
// and immediately jump to and play its first track, interrupting
// whatever is playing. Avoids starting on the current track so the
// click has audible feedback.
func (m *model) reshuffleAndPlay() {
	if len(m.tracks) == 0 {
		return
	}
	curIdx := -1
	for i, t := range m.tracks {
		if t.Path == m.status.File {
			curIdx = i
		}
	}
	m.shuf.on = true
	m.shuf.order = freshBag(len(m.tracks), curIdx)
	m.shuf.pos = 0
	next, rest, ok := pickNext(m.queue, m.tracks, "", &m.shuf)
	m.queue = rest
	if !ok {
		return
	}
	if err := m.be.playFile(next); err != nil {
		m.beErr = err.Error()
	}
	m.syncDetail()
}

// toggleShuffle flips shuffle mode. Turning on while idle starts
// playback immediately on a random track ("hit it and it just
// works"); otherwise only future advances are affected. Turning off
// never interrupts the current track.
func (m *model) toggleShuffle() {
	if m.shuf.on {
		m.shuf.on = false
		m.shuf.order = nil
		m.shuf.pos = 0
		return
	}
	if len(m.tracks) == 0 {
		return
	}
	m.shuf.on = true
	m.shuf.order = freshBag(len(m.tracks), -1)
	m.shuf.pos = 0
	if m.status.File == "" {
		next, rest, ok := pickNext(m.queue, m.tracks, "", &m.shuf)
		m.queue = rest
		if !ok {
			return
		}
		if err := m.be.playFile(next); err != nil {
			m.beErr = err.Error()
		}
	}
}

// queueMove swaps item i with its neighbor in dir (-1 up, +1 down);
// the cursor follows the moved item. Boundary moves are no-ops.
// In-place on the live queue slice pickNext() drains — no copy, no
// parallel representation.
func queueMove(q []string, i, dir int) ([]string, int) {
	j := i + dir
	if i < 0 || i >= len(q) || j < 0 || j >= len(q) {
		return q, min(max(i, 0), max(0, len(q)-1))
	}
	q[i], q[j] = q[j], q[i]
	return q, j
}

// queueRemove drops item i, clamping the cursor into range.
func queueRemove(q []string, i int) ([]string, int) {
	if len(q) == 0 {
		return q, 0
	}
	i = min(max(i, 0), len(q)-1)
	q = append(q[:i], q[i+1:]...)
	return q, min(i, max(0, len(q)-1))
}

func (m *model) clampQCursor() {
	if len(m.queue) == 0 {
		m.qCursor = 0
		return
	}
	m.qCursor = min(max(m.qCursor, 0), len(m.queue)-1)
}

func (m *model) enqueueCursor() {
	if len(m.view) == 0 {
		return
	}
	m.clampCursor()
	m.queue = append(m.queue, m.view[m.cursor].Path)
}

func (m *model) playCursor() {
	if len(m.view) == 0 {
		return
	}
	m.clampCursor()
	if err := m.be.playFile(m.view[m.cursor].Path); err != nil {
		m.beErr = err.Error()
	}
}

// handleMouse maps clicks/wheel onto list rows. Mouse events only
// arrive with WithMouseCellMotion; if the terminal never sends them,
// every path below simply never fires and keyboard remains complete.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	listTop, listLeft, _ := m.listGeometry()
	inList := msg.X >= listLeft+1 && msg.X < listLeft+1+m.listInnerWidth() &&
		msg.Y >= listTop+2 && msg.Y < listTop+2+m.visibleRows()
	inDetail := !inList && msg.X >= 2+m.listPaneW() &&
		msg.Y < m.height-m.statusH()
	// Clickable status regions: progress bar (seek) on the first
	// content line, transport buttons on the second.
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		barY, barX0, barX1 := m.barZone()
		if msg.Y == barY && msg.X >= barX0 && msg.X < barX1 && m.status.Duration > 0 {
			frac := float64(msg.X-barX0) / float64(max(1, barX1-barX0))
			if frac < 0 {
				frac = 0
			}
			if frac > 1 {
				frac = 1
			}
			target := int(frac * float64(m.status.Duration))
			_ = m.be.seekAbs(target)
			return m, nil
		}
	}
	// Status-bar transport buttons live on the second content line:
	// one border row + bar line above it.
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress &&
		msg.Y == m.height-2 {
		_, zones := m.statusLine()
		for _, z := range zones {
			if msg.X >= z.x0+1 && msg.X < z.x1+1 {
				m.flash = z.action
				switch z.action {
				case "prev":
					m.playPrev()
				case "play":
					_ = m.be.toggle()
				case "next":
					m.playNext()
				case "shuffle":
					m.reshuffleAndPlay()
				}
				return m, tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
					return flashMsg{}
				})
			}
		}
		return m, nil
	}
	switch {
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		if inList {
			m.cursor -= 3
			m.clampCursor()
		} else if inDetail {
			m.detail.LineUp(3)
		}
		return m, nil
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		if inList {
			m.cursor += 3
			m.clampCursor()
		} else if inDetail {
			m.detail.LineDown(3)
		}
		return m, nil
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && inList:
		m.focus = focusList
		m.layoutPanes()
		// Map the screen row through the liner-notes window:
		// headings and gaps are not selectable, tracks resolve to
		// their view index.
		lines := m.listWindow()
		sr := msg.Y - (listTop + 2)
		if sr >= 0 && sr < len(lines) && lines[sr].kind == llTrack {
			row := lines[sr].idx
			now := time.Now()
			if row == m.lastRow && now.Sub(m.lastClick) < 500*time.Millisecond {
				m.cursor = row
				m.playCursor()
				m.lastClick = time.Time{}
			} else {
				m.cursor = row
				m.clampCursor()
				m.lastClick, m.lastRow = now, row
			}
		}
		return m, nil
	}
	return m, nil
}

// ---- geometry: must match View exactly or clicks land wrong ----

func (m model) listGeometry() (top, left, width int) {
	// Rows start below the outer frame + title; list spans its pane.
	return 0, 0, m.listPaneW()
}

// One frame, not three boxes: a single heavy outer border, light
// internal dividers. listPaneW is the library content width (no box);
// rightPaneW is everything right of the divider.
func (m model) listPaneW() int { return max(20, (m.width-2)*60/100) }
func (m model) rightPaneW() int {
	return max(20, m.width-2-m.listPaneW()-1)
}
func (m model) listInnerWidth() int { return max(1, m.listPaneW()) }

// statusH is the bottom reservation: divider + bar + buttons + the
// outer frame's own row. visibleRows hangs off it.
func (m model) statusH() int { return 4 }

type vizTickMsg struct{}

// vizBars returns the bar count for the current width: one column +
// one gutter per bar.
func (m model) vizBars() int {
	inner := m.rightPaneW()
	return max(8, min(48, inner/2))
}

// shouldViz gates all FFT work: playing, focused, backend healthy.
func (m model) shouldViz() bool {
	return m.status.State == "playing" && m.focused && m.beErr == ""
}

// syncViz starts/stops the tap + tick loop on state transitions.
// Idempotent: steady state returns nil.
func (m *model) syncViz() tea.Cmd {
	want := m.shouldViz()
	if want && !m.vizActive {
		m.tap.start()
		if _, broken := m.tap.state(); broken != "" {
			m.vizErr = broken
			m.vizActive = false
			return nil
		}
		m.vizErr = ""
		m.vizActive = true
		m.peaks = nil
		return m.vizTick()
	}
	if !want && m.vizActive {
		m.tap.stop()
		m.vizActive = false
		m.levels = nil
		m.peaks = nil
	}
	return nil
}

func (m *model) vizTick() tea.Cmd {
	return tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg {
		return vizTickMsg{}
	})
}

// hexLerp interpolates two #rrggbb colors by t in [0,1].
func hexLerp(a, b string, t float64) (int, int, int) {
	var ar, ag, ab, br, bg, bb int
	fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab)
	fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb)
	return int(float64(ar) + float64(br-ar)*t),
		int(float64(ag) + float64(bg-ag)*t),
		int(float64(ab) + float64(bb-ab)*t)
}

// meterColor maps position t in [0,1] along the shared prim→sec
// ramp. Progress bar (horizontal axis) and visualizer (vertical axis)
// draw through this one function with the same █/─ glyphs: scrubbing
// and watching read as one instrument on one signal.
func meterColor(a, b string, t float64) (int, int, int) {
	return hexLerp(a, b, t)
}

// renderBar draws the progress meter: filled cells in the shared
// gradient, empty cells as quiet ─ rests. Width matches barZone
// exactly so clicks land where the cells render.
func (m model) renderBar(pct float64) string {
	w := max(10, m.width-30)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	a, b := m.vizAccent()
	filled := int(pct * float64(w))
	var sb strings.Builder
	for i := 0; i < w; i++ {
		if i < filled {
			cr, cg, cb := meterColor(a, b, (float64(i)+0.5)/float64(w))
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\u2588", cr, cg, cb)
		} else {
			cr, cg, cb := surfaceRGB()
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\u2500", cr, cg, cb)
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// surfaceRGB parses the surface token for quiet meter cells.
func surfaceRGB() (int, int, int) {
	var r, g, b int
	fmt.Sscanf(string(colSurface), "#%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

// vizAccent returns the live bar colors: current track accent pair,
// falling back to theme tokens.
func (m model) vizAccent() (string, string) {
	a, b := m.artPrim, m.artSec
	if a == "" {
		a = themeBase.Accent
	}
	if b == "" {
		b = themeBase.Accent2
	}
	return a, b
}

// renderViz builds the fixed-height strip: gradient bars with
// peak-hold markers, idle/error states when the loop is off.
func (m model) renderViz() string {
	bars := m.vizBars()
	inner := m.rightPaneW()
	if !m.vizActive {
		if m.vizErr != "" {
			return styleError.Render("visualizer: "+m.vizErr) + "\n" + strings.Repeat("\n", vizHeight-1)
		}
		// Resting baseline, compact single line (not a 4-row block):
		// flat zero-height bars in the live accent color. Reads as
		// "resting," and hands its rows to the art box above.
		a, _ := m.vizAccent()
		var cr, cg, cb int
		fmt.Sscanf(a, "#%02x%02x%02x", &cr, &cg, &cb)
		var sb strings.Builder
		fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", cr, cg, cb)
		for i := 0; i < bars; i++ {
			sb.WriteString("─ ")
		}
		sb.WriteString("\x1b[0m\n")
		return sb.String()
	}
	a, b := m.vizAccent()
	var sb strings.Builder
	for r := vizHeight - 1; r >= 0; r-- {
		frac := float64(r+1) / float64(vizHeight)
		cr, cg, cb := hexLerp(a, b, frac)
		for i := 0; i < bars && i*2 < inner; i++ {
			lvl := 0.0
			if i < len(m.levels) {
				lvl = m.levels[i]
			}
			peak := 0.0
			if i < len(m.peaks) {
				peak = m.peaks[i]
			}
			h := lvl * float64(vizHeight)
			prow := peak * float64(vizHeight)
			switch {
			case float64(r)+1 <= h:
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm█", cr, cg, cb)
			case prow > h && float64(r) < prow && float64(r)+1 >= prow:
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm─", cr, cg, cb)
			default:
				sb.WriteString(" ")
			}
			sb.WriteString(" ")
		}
		sb.WriteString("\x1b[0m\n")
	}
	return sb.String()
}

func (m *model) layoutPanes() {
	rightW := m.rightPaneW()
	m.detail.Width = max(10, rightW)
	m.syncDetail()
}

// vizReserve is the strip's row cost for layout: a single resting
// line when idle, the full vizHeight when active or erroring.
func (m model) vizReserve() int {
	if !m.vizActive && m.vizErr == "" {
		return 1
	}
	return vizHeight
}

// maxTextH is the tallest the Now Playing viewport may grow: whatever
// remains after the art and visualizer reservations.
func (m model) maxTextH() int {
	rightInnerH := m.topH()
	_, artRows := m.artBox()
	return max(3, rightInnerH-artRows-m.vizReserve())
}

// applyAccent pushes a per-track accent into selection, progress and
// focus styles. No-op when unchanged.
func (m *model) applyAccent(prim, sec string) {
	if prim == "" {
		prim = themeBase.Accent
	}
	if sec == "" {
		sec = themeBase.Accent2
	}
	key := prim + "|" + sec
	if key == m.appliedAc {
		return
	}
	m.appliedAc = key
	colAccent = lipgloss.Color(prim)
	colAccent2 = lipgloss.Color(sec)
	if prim == themeBase.Accent && sec == themeBase.Accent2 {
		colBG = lipgloss.Color(themeBase.Bg)
	} else {
		colBG = lipgloss.Color(bgTint(prim))
	}
	styleSelected = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#161310")).Bold(true)
	stylePlaying = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHL = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleArtist = lipgloss.NewStyle().Foreground(colAccent2).Bold(true)
	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent).Background(colBG)
	styleBlurBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colSurface).Background(colBG)
	styleOuterFrame = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(colMuted).Background(colBG)
}

// artBox returns the art render box: full inner width, ~50% of the
// right-pane inner height. Reclaimed dead space (text tightening +
// compact idle viz, one row instead of four) went here: a bigger
// render target is what fixes the stair-stepping on diagonals, which
// is a resolution ceiling, not a renderer technique problem.
func (m model) artBox() (cols, rows int) {
	cols = max(10, m.rightPaneW())
	if m.bgMode() {
		return cols, 0
	}
	innerH := m.topH()
	rows = innerH * 50 / 100
	rows = min(rows, max(4, innerH-10))
	rows = max(4, rows)
	return cols, rows
}

// bgMode reports whether the blurred-background experiment is active:
// enabled in config AND art actually present for this track.
func (m model) bgMode() bool {
	return m.cfg.Theme.BackgroundArt && m.artImg != nil
}

// loadBg builds (or loads from disk cache) the blurred+scrimmed
// background variant for path, from the already-cached pixels.
func (m *model) loadBg(path string) {
	m.bgImg = nil
	key := artKey(path)
	if key == "" {
		return
	}
	bgPath := filepath.Join(artDir(), key+"-"+artCacheVer+"-bg.png")
	if raw, err := os.ReadFile(bgPath); err == nil {
		if img, _, err := image.Decode(bytes.NewReader(raw)); err == nil {
			if rgba, ok := img.(*image.RGBA); ok {
				m.bgImg = rgba
			} else {
				m.bgImg = toRGBA(img)
			}
			return
		}
	}
	img, _, _, err := cachedArt(path)
	if err != nil || img == nil {
		return
	}
	blurred := blurCached(toRGBA(img))
	scr := scrimToward(blurred, themeBase.Bg, 0.35)
	m.bgImg = scr
	var buf bytes.Buffer
	if err := encodePNG(&buf, scr); err == nil {
		_ = os.MkdirAll(artDir(), 0o755)
		_ = os.WriteFile(bgPath, buf.Bytes(), 0o644)
		_ = os.Remove(filepath.Join(artDir(), key+"-bg.png"))
	}
}

// renderBgText draws the Now Playing text over the blurred art: each
// cell takes its background from the art, text glyphs draw in the
// theme text color on top. Bypasses the viewport (no scrolling in
// background mode — content is short by construction).
func (m model) renderBgText() string {
	lines := strings.Split(m.detailText(), "\n")
	W := m.detail.Width
	H := m.maxTextH()
	var fr, fg, fb int
	fmt.Sscanf(themeBase.Text, "#%02x%02x%02x", &fr, &fg, &fb)
	sb := m.bgImg.Bounds()
	var b strings.Builder
	lastBG := -1
	for r := 0; r < H; r++ {
		var runes []rune
		if r < len(lines) {
			runes = []rune(stripANSI(lines[r]))
		}
		for c := 0; c < W; c++ {
			sx := sb.Min.X + c*sb.Dx()/max(1, W)
			sy := sb.Min.Y + r*sb.Dy()/max(1, H)
			br, bg, bb, _ := m.bgImg.At(sx, sy).RGBA()
			bgi := int(br>>8)<<16 | int(bg>>8)<<8 | int(bb>>8)
			if bgi != lastBG {
				fmt.Fprintf(&b, "\x1b[48;2;%d;%d;%dm", br>>8, bg>>8, bb>>8)
				lastBG = bgi
			}
			if c < len(runes) {
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%c", fr, fg, fb, runes[c])
			} else {
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
		lastBG = -1
	}
	return b.String()
}

// stripANSI removes escape sequences so overlay text measures cleanly.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// loadArt refreshes cached pixels + accents for path and rebuilds the
// art block. Decode/resize/sample happen only on cache miss inside
// cachedArt.
func (m *model) loadArt(path string) {
	m.artFile = path
	img, prim, sec, err := cachedArt(path)
	if err != nil || img == nil {
		m.artImg = nil
		m.artPrim, m.artSec = "", ""
		m.applyAccent("", "")
		m.artBlock = m.emptyArt()
		return
	}
	m.artImg = img
	m.artPrim, m.artSec = prim, sec
	m.applyAccent(prim, sec)
	if m.bgMode() {
		m.loadBg(path)
		m.artBlock = ""
	} else {
		m.bgImg = nil
		m.artBlock = m.renderArt()
	}
}

func (m model) emptyArt() string {
	_, rows := m.artBox()
	var b strings.Builder
	b.WriteString(styleMuted.Render(iconPlaying+" no cover art") + "\n")
	for i := 1; i < rows; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderArt maps cached pixels to the current box. Cheap: no decode,
// no resize from source, no sampling.
func (m model) renderArt() string {
	if m.artImg == nil {
		return m.emptyArt()
	}
	cols, rows := m.artBox()
	if m.kitty {
		return m.renderKitty(cols, rows)
	}
	sb := m.artImg.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, cols, rows*2))
	draw.CatmullRom.Scale(dst, dst.Bounds(), m.artImg, sb, draw.Over, nil)
	return renderHalfBlock(dst, cols, rows)
}

// renderKitty emits transmit+display frames sized near the box, then
// blank lines reserving the space (terminal overlays don't flow text).
// UNTESTED against a real Kitty-capable terminal (none on this machine);
// half-block above is the supported path here.
func (m model) renderKitty(cols, rows int) string {
	pngBytes, w, h := m.artPNG()
	if pngBytes == nil {
		return m.emptyArt()
	}
	m.kittyID++
	var b strings.Builder
	for _, f := range kittyPNGFrames(pngBytes, w, h, m.kittyID) {
		b.WriteString(f)
	}
	b.WriteString("\n")
	for i := 1; i < rows; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// artPNG re-encodes cached pixels for Kitty upload.
func (m model) artPNG() ([]byte, int, int) {
	if m.artImg == nil {
		return nil, 0, 0
	}
	var buf bytes.Buffer
	if err := encodePNG(&buf, m.artImg); err != nil {
		return nil, 0, 0
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, 0, 0
	}
	return buf.Bytes(), cfg.Width, cfg.Height
}

func (m *model) syncDetail() {
	content := m.detailText()
	// Fit the viewport to its content so no dead blank gap pools
	// between the text and the visualizer strip. Every content line
	// ends with newline, so count exact lines without a +1 (a +1 here
	// leaked one blank row per render — measured in a live frame).
	// Scrolling still works when content exceeds the reservation.
	trimmed := strings.TrimRight(content, "\n")
	lines := 1
	if trimmed != "" {
		lines = strings.Count(trimmed, "\n") + 1
	}
	m.detail.Height = min(max(3, lines), m.maxTextH())
	m.detail.SetContent(content)
}

// detailText builds the Now Playing text block. Shared by the
// viewport path and the background-art renderer so both show the
// same content by construction.
func (m *model) detailText() string {
	var b strings.Builder
	if m.beErr != "" && m.status.File == "" {
		b.WriteString(styleError.Render("backend: "+m.beErr) + "\n")
	}
	if m.status.File == "" {
		b.WriteString(styleMuted.Render("nothing playing") + "\n")
		b.WriteString(styleMuted.Render("Enter on a track to play") + "\n")
	} else {
		b.WriteString(styleTitle.Render("Now Playing") + "\n")
		artist := m.status.Artist
		if artist == "" {
			artist = "—"
		}
		fmt.Fprintf(&b, "%s\n", styleSelected.Render(" "+artist+" "))
		fmt.Fprintf(&b, "%s\n", m.status.Title)
		if m.status.Album != "" {
			fmt.Fprintf(&b, "%s\n", styleMuted.Render(m.status.Album))
		}
		fmt.Fprintf(&b, "%s\n", styleMuted.Render(m.status.File))
	}
	if len(m.queue) > 0 {
		fmt.Fprintf(&b, "\nQueue (%d)\n", len(m.queue))
	}
	if m.shuf.on {
		fmt.Fprintf(&b, "%s\n", stylePlaying.Render(iconShuffle+" shuffle on"))
	}
	content := b.String()
	return content
}

func fmtTime(s int) string {
	if s < 0 {
		s = 0
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// helpView renders the keybind overlay from cfg.keySets — the same
// effective map the config file produces, so docs can't drift.
func (m model) helpView() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("keys") + "\n\n")
	for _, ks := range m.cfg.keySets() {
		b.WriteString(fmt.Sprintf("%-18s %s\n", ks.action, styleMuted.Render(strings.Join(ks.keys, " "))))
	}
	b.WriteString("\n" + styleMuted.Render("? / esc closes"))
	box := styleFocusedBorder.Padding(1, 3).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// queueView renders the live play queue as a centered modal (same
// mechanism as the help overlay). Reads m.queue directly — the slice
// pickNext() drains — so reorder/remove apply to real playback, and a
// track finishing mid-browse simply shifts the list under the cursor
// (clamped on every navigation key).
func (m model) queueView() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(fmt.Sprintf("queue (%d)", len(m.queue))) + "\n\n")
	if len(m.queue) == 0 {
		b.WriteString(styleMuted.Render("empty — a adds the cursor track") + "\n")
	}
	labels := make(map[string]string, len(m.tracks))
	for _, t := range m.tracks {
		labels[t.Path] = t.label()
	}
	qi := min(max(m.qCursor, 0), max(0, len(m.queue)-1))
	for i, qp := range m.queue {
		lab := labels[qp]
		if lab == "" {
			lab = baseName(qp)
		}
		line := fmt.Sprintf("%2d  %s", i+1, lab)
		if i == qi && len(m.queue) > 0 {
			b.WriteString(styleSelected.Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + styleMuted.Render("j/k select · J/K move · d remove · esc closes"))
	box := styleFocusedBorder.Padding(1, 3).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// Instrument iconography. Every glyph below is verified PRESENT in
// JetBrainsMono Nerd Font itself (fc-match "JetBrainsMono Nerd
// Font Mono:charset=<hex>" resolves to the JBM file, not a fallback)
// — the old ⇄ never passed that check (DejaVu fallback), and neither
// did ⏮⏭⏸ (Adwaita) nor ♪ (DejaVu). The SMuFL musical-notation block
// (U+1D15F et al.) was auditioned and REJECTED for the same reason: it
// resolves to Noto Music at mismatched metrics. So: the note marks
// the sounding voice (playing row, empty-art placeholder), transport
// uses Font Awesome's in-font step/play/pause, shuffle is FA random.
// All single-cell, so button geometry is untouched.
const (
	iconPlaying = "\uf001" // FA music note: the sounding track
	iconPrev    = "\uf048" // FA step-backward
	iconPlay    = "\uf04b" // FA play
	iconPause   = "\uf04c" // FA pause
	iconNext    = "\uf051" // FA step-forward
	iconShuffle = "\uf074" // FA random: shuffle
)

// btnZone is a clickable status-bar region: rune-width offsets within
// the status content line (border adds +1 to X at hit-test time).
type btnZone struct {
	x0, x1 int
	action string
}

type flashMsg struct{}

// barZone tracks the progress bar's rendered region, sharing the
// runewidth geometry approach with the transport buttons.
func (m model) barZone() (y, x0, x1 int) {
	w := max(10, m.width-30)
	return m.height - 3, 1, 1 + w
}

// statusLine builds the second status-bar line and its button zones.
// Pure function of width/state: View and handleMouse share it so clicks
// always land where the buttons render.
func (m model) statusLine() (string, []btnZone) {
	var b strings.Builder
	var zones []btnZone
	pos := 0
	button := func(glyph, action string) {
		cell := " " + glyph + " "
		zones = append(zones, btnZone{pos, pos + runewidth.StringWidth(cell), action})
		if m.flash == action {
			cell = styleSelected.Render(cell)
		}
		b.WriteString(cell)
		pos += runewidth.StringWidth(cell)
	}
	play := iconPlay
	if m.status.State == "paused" {
		play = iconPause
	}
	button(iconPrev, "prev")
	b.WriteString(" ")
	pos++
	button(play, "play")
	b.WriteString(" ")
	pos++
	button(iconNext, "next")
	b.WriteString(" ")
	pos++
	shufCell := " " + iconShuffle + " "
	zones = append(zones, btnZone{pos, pos + runewidth.StringWidth(shufCell), "shuffle"})
	if m.shuf.on {
		shufCell = styleSelected.Render(shufCell)
	}
	b.WriteString(shufCell)
	pos += runewidth.StringWidth(shufCell)
	rest := fmt.Sprintf(" │ %s / %s   %s",
		fmtTime(m.status.Position), fmtTime(m.status.Duration), m.status.State)
	if m.status.State == "" {
		rest = " │ stopped"
	}
	if m.beErr != "" {
		rest += "   " + styleError.Render(m.beErr)
	}
	b.WriteString(rest)
	return b.String(), zones
}

func (m model) View() string {
	if m.showHelp {
		return m.helpView()
	}
	if m.showQueue {
		return m.queueView()
	}
	if m.width <= 0 {
		return "loading…"
	}
	if m.indexing {
		return styleMuted.Render("indexing library…")
	}
	if m.indexErr != "" {
		return styleError.Render("library: " + m.indexErr)
	}
	listW, rightW := m.listPaneW(), m.rightPaneW()
	contentW := listW + 1 + rightW
	topH := m.topH()
	// Library list as liner notes: artist headings carry the
	// structure, quiet title rows sit beneath. (Searching renders a
	// flat fuzzy list instead — see listWindow.)
	var rows []string
	vis := m.visibleRows()
	playing := m.playingPath()
	for _, ln := range m.listWindow() {
		switch ln.kind {
		case llGap:
			rows = append(rows, "")
		case llArtist:
			a := m.view[ln.idx].Artist
			if a == "" {
				a = "—"
			}
			rows = append(rows, styleArtist.Render(strings.ToUpper(a)))
		default:
			i := ln.idx
			t := m.view[i]
			// One style per row, never nested: nested lipgloss spans
			// emit mid-line resets that fracture the outer row style
			// (verified: partial highlight blocks + gaps). Playing
			// rows and the cursor row render plain text under their
			// own style; fuzzy spans only ever appear on otherwise-
			// unstyled rows, and only while a search is active
			// (viewHL is cleared with the query).
			if t.Path == playing && playing != "" {
				line := iconPlaying + " " + t.Title
				if i == m.cursor {
					rows = append(rows, styleSelected.Render(line))
				} else {
					rows = append(rows, stylePlaying.Render(line))
				}
				continue
			}
			if i == m.cursor {
				rows = append(rows, styleSelected.Render("  "+t.Title))
			} else if m.searching && i < len(m.viewHL) {
				rows = append(rows, "  "+fuzzyLine(t.label(), m.viewHL[i]))
			} else {
				rows = append(rows, "  "+t.Title)
			}
		}
	}
	for len(rows) < vis {
		rows = append(rows, "")
	}
	padCol := func(w int, lines []string) string {
		st := lipgloss.NewStyle().Width(w)
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = st.Render(l)
		}
		return lipgloss.JoinVertical(lipgloss.Left, out...)
	}
	var listTitle string
	if m.searching {
		listTitle = m.searchBox.View()
	} else if m.focus == focusList {
		listTitle = styleHL.Render(fmt.Sprintf(" library (%d)  / search ", len(m.view)))
	} else {
		listTitle = styleMuted.Render(fmt.Sprintf(" library (%d)  / search ", len(m.view)))
	}
	listCol := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Width(listW).Render(listTitle),
		padCol(listW, rows))

	// Right pane, top to bottom: Now Playing text (over blurred art
	// in background mode, else plain viewport), visualizer strip,
	// cover art anchored at the bottom (suppressed in background mode).
	var textPart string
	parts := []string{}
	if m.bgMode() {
		textPart = m.renderBgText()
	} else {
		textPart = m.detail.View()
	}
	parts = append(parts, textPart, m.renderViz())
	if !m.bgMode() {
		parts = append(parts, m.artBlock)
	}
	rightCol := lipgloss.JoinVertical(lipgloss.Left, parts...)
	// Pad the short column so the divider runs the full top height.
	nRight := strings.Count(rightCol, "\n") + 1
	for nRight < topH {
		rightCol += "\n"
		nRight++
	}

	// One frame: heavy outer edge, light internal dividers. Line
	// weight does the hierarchy work — nothing in between.
	var div strings.Builder
	for i := 0; i < topH; i++ {
		div.WriteString("\u2502\n")
	}
	divider := styleDivider.Render(strings.TrimRight(div.String(), "\n"))
	top := lipgloss.JoinHorizontal(lipgloss.Top, listCol, divider, rightCol)

	pct := 0.0
	if m.status.Duration > 0 {
		pct = float64(m.status.Position) / float64(m.status.Duration)
		if pct > 1 {
			pct = 1
		}
	}
	barLine := lipgloss.NewStyle().Width(contentW).Render(m.renderBar(pct))
	statusText, _ := m.statusLine()
	statusText = lipgloss.NewStyle().Width(contentW).Render(statusText)
	hrule := styleDivider.Render(strings.Repeat("\u2500", contentW))
	content := lipgloss.JoinVertical(lipgloss.Left, top, hrule, barLine, statusText)
	return styleOuterFrame.Width(contentW).Render(content)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
