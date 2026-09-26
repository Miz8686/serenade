package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"golang.org/x/image/draw"
)

// Phase-1 theme: single hardcoded Mocha accent set. Dynamic extraction
// is explicitly Phase 2 — see ROADMAP in the project brief.
var (
	colBG      = lipgloss.Color("#1E1E2E")
	colSurface = lipgloss.Color("#313244")
	colText    = lipgloss.Color("#CDD6F4")
	colMuted   = lipgloss.Color("#9399B2")
	colAccent  = lipgloss.Color("#CBA6F7")
	colAccent2 = lipgloss.Color("#89B4FA")
	colError   = lipgloss.Color("#F38BA8")

	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent)
	styleBlurBorder    = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colSurface)
	styleSelected      = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#11111B")).Bold(true)
	stylePlaying       = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleMuted         = lipgloss.NewStyle().Foreground(colMuted)
	styleTitle         = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleError         = lipgloss.NewStyle().Foreground(colError)
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
	searchBox textinput.Model
	focus     focusPane
	width     int
	height    int
	status    Status
	prev      Status // previous poll; detects natural track end
	beErr     string
	// Phase-2 art state. artImg holds cached pixels (never re-decoded);
	// artBlock is the pre-rendered right-pane art section.
	artImg   image.Image
	artPrim  string
	artSec   string
	artBlock string
	artFile  string
	kitty    bool
	kittyID  int
	// appliedAc tracks the last applied "prim|sec" to avoid restyles.
	appliedAc string
	focused   bool // terminal focus; false pauses background polling
	detail    viewport.Model
	bar       progress.Model
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
	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent)
	styleBlurBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colSurface)
	styleSelected = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#11111B")).Bold(true)
	stylePlaying = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleMuted = lipgloss.NewStyle().Foreground(colMuted)
	styleTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleError = lipgloss.NewStyle().Foreground(colError)
}

func newModel(be *backend, kitty bool, cfg Config) model {
	vp := viewport.New(0, 0)
	bar := progress.New(
		progress.WithGradient(string(colAccent), string(colAccent2)),
		progress.WithoutPercentage(),
	)
	themeBase = cfg.Theme
	buildBaseStyles()
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.CharLimit = 64
	return model{
		be:        be,
		cfg:       cfg,
		searchBox: ti,
		kitty:     kitty,
		indexing:  true,
		focused:   true,
		detail:    vp,
		bar:       bar,
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
	// List pane height minus status box, border (2) and title line (1).
	return max(1, m.height-m.statusH()-2-1)
}

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
			// Natural track end: same file, was playing, now stopped.
			// (A manual next() lands on a *different* file, so this
			// cannot double-skip user input.)
			if m.prev.State == "playing" && msg.st.State == "stopped" && m.prev.File != "" && msg.st.File == m.prev.File {
				m.status = msg.st
				m.prev = msg.st
				m.syncDetail()
				m.advance()
				if m.focused {
					return m, pollBackend(m.be)
				}
				return m, nil
			}
			m.prev = m.status
			m.status = msg.st
			if m.status.File != m.artFile {
				m.loadArt(m.status.File)
			}
		}
		m.syncDetail()
		if m.focused {
			return m, pollBackend(m.be)
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, pollBackend(m.be)

	case tea.BlurMsg:
		// Terminal unfocused: stop scheduling polls. The in-flight
		// statusMsg that arrives simply won't re-arm the ticker.
		m.focused = false
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

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
			b.WriteString(stylePlaying.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	is := m.cfg.keyIs
	// Search mode captures everything except Enter (play+exit) and
	// Esc (exit, restore full list). Playback keys stay silent here
	// so typing a space doesn't toggle pause mid-query.
	if m.searching {
		switch {
		case k == "enter":
			m.searching = false
			m.searchBox.Blur()
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
		return m, tea.Quit
	case is("toggle", k):
		_ = m.be.toggle()
		return m, nil
	case is("next", k):
		_ = m.be.next()
		return m, nil
	case is("prev", k):
		_ = m.be.prev()
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
	}
	// Detail pane gets navigation keys when focused.
	if m.focus == focusDetail {
		switch {
		case is("tab", k):
			m.focus = focusList
			m.layoutPanes()
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

// pickNext is the single "what plays next" decision: queued head
// first, otherwise next-in-library-order. Pure for testability.
func pickNext(queue []string, tracks []Track, prevFile string) (next string, rest []string, ok bool) {
	if len(queue) > 0 {
		return queue[0], queue[1:], true
	}
	next, ok = nextTrackPath(tracks, prevFile)
	return next, queue, ok
}

func (m *model) advance() {
	next, rest, ok := pickNext(m.queue, m.tracks, m.prev.File)
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
	inDetail := !inList && msg.X >= m.listOuterWidth() &&
		msg.Y < m.height-m.statusH()
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
		row := m.offset + (msg.Y - (listTop + 2))
		if row >= 0 && row < len(m.tracks) {
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
	// Status bar occupies bottom statusH lines; list spans the rest.
	return 0, 0, m.listOuterWidth()
}
func (m model) listOuterWidth() int { return max(20, m.width*60/100) }
func (m model) listInnerWidth() int { return max(1, m.listOuterWidth()-2) }
func (m model) statusH() int        { return 4 }

func (m *model) layoutPanes() {
	rightW := max(20, m.width-m.listOuterWidth())
	rightInnerH := max(8, m.height-m.statusH()-2)
	_, artRows := m.artBox()
	m.detail.Width = max(10, rightW-2)
	m.detail.Height = max(3, rightInnerH-artRows)
	m.syncDetail()
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
	styleSelected = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#11111B")).Bold(true)
	stylePlaying = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleFocusedBorder = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colAccent)
	m.bar = progress.New(
		progress.WithGradient(prim, sec),
		progress.WithoutPercentage(),
	)
}

// artBox returns the art render box: full inner width, ~40% of the
// right-pane inner height. Text keeps the rest, with vertical room
// below the block where Phase 3's spectrograph strip will slot in.
func (m model) artBox() (cols, rows int) {
	cols = max(10, m.width-m.listOuterWidth()-2)
	innerH := max(8, m.height-m.statusH()-2)
	rows = innerH * 40 / 100
	rows = min(rows, max(4, innerH-10))
	rows = max(4, rows)
	return cols, rows
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
	m.artBlock = m.renderArt()
}

func (m model) emptyArt() string {
	_, rows := m.artBox()
	var b strings.Builder
	b.WriteString(styleMuted.Render("♪ no cover art") + "\n")
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
	var b strings.Builder
	if m.beErr != "" && m.status.File == "" {
		b.WriteString(styleError.Render("backend: "+m.beErr) + "\n")
	}
	if m.status.File == "" {
		b.WriteString(styleMuted.Render("nothing playing") + "\n")
		b.WriteString(styleMuted.Render("Enter on a track to play") + "\n")
	} else {
		b.WriteString(styleTitle.Render("Now Playing") + "\n\n")
		artist := m.status.Artist
		if artist == "" {
			artist = "—"
		}
		fmt.Fprintf(&b, "%s\n", styleSelected.Render(" "+artist+" "))
		fmt.Fprintf(&b, "\n%s\n", m.status.Title)
		if m.status.Album != "" {
			fmt.Fprintf(&b, "%s\n", styleMuted.Render(m.status.Album))
		}
		fmt.Fprintf(&b, "\n%s\n", styleMuted.Render(m.status.File))
	}
	if len(m.queue) > 0 {
		fmt.Fprintf(&b, "\nQueue (%d)\n", len(m.queue))
	}
	m.detail.SetContent(b.String())
}

func fmtTime(s int) string {
	if s < 0 {
		s = 0
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (m model) View() string {
	if m.width <= 0 {
		return "loading…"
	}
	if m.indexing {
		return styleMuted.Render("indexing library…")
	}
	if m.indexErr != "" {
		return styleError.Render("library: " + m.indexErr)
	}
	// Library list (current filter view, with fuzzy highlights).
	var rows []string
	vis := m.visibleRows()
	playing := m.playingPath()
	for i := m.offset; i < m.offset+vis && i < len(m.view); i++ {
		t := m.view[i]
		base := t.label()
		if m.searching && i < len(m.viewHL) {
			base = fuzzyLine(t.label(), m.viewHL[i])
		}
		line := "  " + base
		if t.Path == playing && playing != "" {
			line = "▶ " + base
			if i == m.cursor {
				rows = append(rows, styleSelected.Render(line))
			} else {
				rows = append(rows, stylePlaying.Render(line))
			}
			continue
		}
		if i == m.cursor {
			rows = append(rows, styleSelected.Render(line))
		} else {
			rows = append(rows, " "+line)
		}
	}
	for len(rows) < vis {
		rows = append(rows, "")
	}
	listBody := lipgloss.JoinVertical(lipgloss.Left, rows...)
	var listTitle string
	if m.searching {
		listTitle = m.searchBox.View()
	} else {
		listTitle = styleMuted.Render(fmt.Sprintf(" library (%d)  / search ", len(m.view)))
	}
	list := lipgloss.JoinVertical(lipgloss.Left, listTitle, listBody)
	if m.focus == focusList {
		list = styleFocusedBorder.Width(m.listOuterWidth() - 2).Render(list)
	} else {
		list = styleBlurBorder.Width(m.listOuterWidth() - 2).Render(list)
	}

	// Right pane: art block on top, Now Playing text below it, with
	// vertical room left under the block for Phase 3's spectrograph.
	right := lipgloss.JoinVertical(lipgloss.Left, m.artBlock, m.detail.View())
	if m.focus == focusDetail {
		right = styleFocusedBorder.Width(m.detail.Width).Render(right)
	} else {
		right = styleBlurBorder.Width(m.detail.Width).Render(right)
	}
	pct := 0.0
	if m.status.Duration > 0 {
		pct = float64(m.status.Position) / float64(m.status.Duration)
		if pct > 1 {
			pct = 1
		}
	}
	m.bar.Width = max(10, m.width-30)
	barLine := m.bar.ViewAs(pct)
	state := m.status.State
	if state == "" {
		state = "stopped"
	}
	statusText := fmt.Sprintf("%s  %s / %s   %s",
		map[string]string{"playing": "▶", "paused": "⏸"}[state],
		fmtTime(m.status.Position), fmtTime(m.status.Duration), state)
	if m.beErr != "" {
		statusText += "   " + styleError.Render(m.beErr)
	}
	// Compose: list left, art+detail right-top, status full-width bottom.
	top := lipgloss.JoinHorizontal(lipgloss.Top, list, right)
	return lipgloss.JoinVertical(lipgloss.Left, top,
		styleBlurBorder.Width(m.width-2).Render(
			lipgloss.JoinVertical(lipgloss.Left, barLine, statusText)))
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
