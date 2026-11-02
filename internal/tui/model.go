// Package tui is darshan's screen: a page, picture or Markdown file with vim
// keys, a search over the whole system with a preview, and the talk with the
// agent that opened the file.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Theyashsawarkar/darshan/internal/doc"
	"github.com/Theyashsawarkar/darshan/internal/state"
	"github.com/Theyashsawarkar/darshan/internal/ui"
)

// Options start the screen.
type Options struct {
	File   string
	Page   int
	Popup  bool   // in a tmux popup: q closes it, z reopens it another size
	Client string // the tmux client the popup is on
	Exe    string // this program, to reopen the popup
}

type mode int

const (
	modeRead mode = iota
	modeSearch
	modeCommand
	modeFind
	modeTalk
	modeHelp
)

// Model is the screen.
type Model struct {
	opt  Options
	st   ui.Styles
	g    gfx
	s    state.State
	w, h int
	mode mode
	msg  string

	file, kind string
	stamp      string
	page       int
	pages      int
	lines      []string // Markdown, drawn
	plain      []string // the same without styling, to search
	scroll     int
	loadErr    string

	zoom     float64
	fitWidth bool
	panX     int
	panY     int
	last     shot
	img      []string
	imgCols  int
	gen      int
	id       uint32
	png      string

	count    string
	pendingG bool
	input    []rune

	query    string
	matches  []int
	matchAt  int
	pageText []string

	find     *finder
	talkOpen bool
	quitting bool
	reopen   bool
}

const (
	viewID1    uint32 = 0xD45201
	viewID2    uint32 = 0xD45202
	previewID1 uint32 = 0xD45203
	previewID2 uint32 = 0xD45204
)

// New makes the screen, on the given file or the last one left open.
func New(o Options) Model {
	m := Model{opt: o, st: ui.New(ui.LoadTheme("darshan")), g: newGfx(o.Client), s: state.Load(), zoom: 1, id: viewID1, page: 1}
	file, page := o.File, o.Page
	if file == "" {
		file, page = m.s.File, m.s.Page
	}
	if file != "" {
		if err := m.load(file, page); err != nil {
			m.msg = err.Error()
			m.file, m.kind = "", ""
		}
	}
	if m.file == "" {
		m.openFind()
	}
	return m
}

func (m *Model) load(path string, page int) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("can't open %s: %v", path, err)
	}
	kind := doc.KindOf(abs)
	if kind == "" {
		return fmt.Errorf("darshan shows PDFs, images and Markdown, not %s", filepath.Base(abs))
	}
	pages := 1
	if kind == doc.PDF {
		if pages, err = doc.PDFPages(abs); err != nil {
			return err
		}
	}
	if m.file != "" && m.file != abs {
		m.s.Pages[m.file] = m.place()
	}
	if page <= 0 {
		page = m.s.Pages[abs]
	}
	m.file, m.kind, m.pages, m.stamp = abs, kind, pages, doc.Stamp(abs)
	m.zoom, m.fitWidth, m.panX, m.panY = 1, false, 0, 0
	m.query, m.matches, m.pageText, m.lines, m.plain = "", nil, nil, nil, nil
	m.img, m.loadErr, m.scroll, m.page = nil, "", 0, 1
	if kind == doc.Markdown {
		m.renderMarkdown()
		m.scroll = max(0, min(page-1, len(m.lines)-1))
	} else {
		m.page = max(1, min(max(page, 1), pages))
	}
	m.s.File = abs
	m.s.Remember(abs)
	m.save()
	return nil
}

func (m *Model) place() int {
	if m.kind == doc.Markdown {
		return m.scroll + 1
	}
	return m.page
}

func (m *Model) save() {
	m.s.Page = m.place()
	if m.file != "" {
		m.s.Pages[m.file] = m.place()
	}
	// the MCP side writes the agent and the talk: keep what's newest there
	disk := state.Load()
	if m.s.Agent == (state.Agent{}) {
		m.s.Agent = disk.Agent
	}
	_ = state.Save(m.s)
}

func (m *Model) renderMarkdown() {
	cols, _ := m.area()
	if cols <= 0 {
		cols = 80
	}
	lines, err := doc.RenderMarkdown(m.file, min(cols-2, 100))
	if err != nil {
		m.loadErr = err.Error()
		return
	}
	m.lines, m.plain = lines, make([]string, len(lines))
	for i, l := range lines {
		m.plain[i] = ansi.Strip(l)
	}
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the clock that notices a changed file.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick()}
	if m.find != nil {
		cmds = append(cmds, m.find.scan())
	}
	return tea.Batch(cmds...)
}

func (m Model) talkWidth() int {
	if !m.talkOpen || m.mode == modeFind {
		return 0
	}
	return max(30, m.w*36/100)
}

// area is the room for the page, in cells.
func (m Model) area() (cols, rows int) {
	cols, rows = m.w-m.talkWidth(), m.h
	if !m.s.Zen {
		rows -= 2 // the top line and the keys
	}
	return max(cols, 1), max(rows, 1)
}

func (m *Model) viewJob() job {
	cols, rows := m.area()
	m.gen++
	return job{Path: m.file, Kind: m.kind, Page: m.page, Cols: cols, Rows: rows, CW: m.g.cw, CH: m.g.ch,
		Zoom: m.zoom, FitWidth: m.fitWidth, PanX: m.panX, PanY: m.panY, Invert: m.s.Invert, Gen: m.gen, Kitty: m.g.kitty}
}

// redraw asks for the current view.
func (m *Model) redraw() tea.Cmd {
	if m.w == 0 || m.file == "" || m.kind == doc.Markdown {
		return nil
	}
	j := m.viewJob()
	cmds := []tea.Cmd{renderCmd(j)}
	if m.kind == doc.PDF && m.page < m.pages {
		n := j
		n.Page = m.page + 1
		cmds = append(cmds, prerender(n))
	}
	return tea.Batch(cmds...)
}

// Update handles everything that happens.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if m.kind == doc.Markdown && m.file != "" {
			m.renderMarkdown()
		}
		cmd := m.redraw()
		if m.find != nil {
			return m, tea.Batch(cmd, m.find.preview(&m))
		}
		return m, cmd
	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if m.file != "" {
			if st := doc.Stamp(m.file); st != m.stamp && st != "" {
				m.stamp, m.pageText, m.matches = st, nil, nil
				if m.kind == doc.PDF {
					if n, err := doc.PDFPages(m.file); err == nil {
						m.pages = n
						m.page = min(m.page, n)
					}
				} else if m.kind == doc.Markdown {
					m.renderMarkdown()
				}
				m.msg = "the file changed: reloaded"
				cmds = append(cmds, m.redraw())
			}
		}
		return m, tea.Batch(cmds...)
	case shotMsg:
		return m.gotShot(shot(msg))
	case cmdMsg:
		return m.command(msg)
	case delivered:
		m.msg = msg.text
		return m, nil
	case batchMsg, scanDone, previewDue:
		if m.find != nil {
			return m.find.update(&m, msg)
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) gotShot(r shot) (tea.Model, tea.Cmd) {
	if r.Preview {
		if m.find != nil {
			m.find.gotShot(&m, r)
		}
		return m, nil
	}
	if r.Gen != m.gen {
		if r.PNG != "" {
			_ = os.Remove(r.PNG)
		}
		return m, nil
	}
	if r.Err != nil {
		m.loadErr = r.Err.Error()
		m.img = nil
		return m, nil
	}
	m.loadErr, m.last, m.panX, m.panY = "", r, r.PanX, r.PanY
	m.imgCols = r.ViewCols
	if r.PNG != "" {
		old, oldPNG := m.id, m.png
		m.id = viewID1
		if old == viewID1 {
			m.id = viewID2
		}
		m.g.place(m.id, r.PNG, r.ViewCols, r.ViewRows)
		m.img, m.png = placeholders(m.id, r.ViewCols, r.ViewRows), r.PNG
		m.g.forget(old)
		if oldPNG != "" {
			_ = os.Remove(oldPNG)
		}
	} else {
		m.img = r.Blocks
	}
	return m, nil
}

// ---- commands from other processes (the CLI, agents)

type cmdMsg struct {
	c     state.Command
	reply chan state.Response
}

func (m Model) status() state.Status {
	st := state.Status{Running: true, File: m.file, Kind: m.kind, Page: m.place(), Pages: m.pages}
	if m.kind == doc.Markdown {
		st.Pages = len(m.lines)
	}
	st.Visible = m.visibleText()
	return st
}

func (m Model) command(c cmdMsg) (tea.Model, tea.Cmd) {
	r := state.Response{OK: true}
	var cmd tea.Cmd
	switch c.c.Cmd {
	case "open":
		if err := m.load(c.c.Path, c.c.Page); err != nil {
			r = state.Response{Error: err.Error()}
			break
		}
		if c.c.Agent.Name != "" || c.c.Agent.Pane != "" {
			m.s.Agent = c.c.Agent
			m.save()
		}
		if m.find != nil {
			m.closeFind()
		}
		if m.mode == modeHelp || m.mode == modeSearch || m.mode == modeCommand {
			m.mode = modeRead
		}
		if c.c.Why != "" {
			m.msg = c.c.Why
		}
		cmd = m.redraw()
	case "reply":
		who := m.s.Agent.Name
		if who == "" {
			who = "agent"
		}
		if c.c.Agent.Name != "" {
			who = c.c.Agent.Name
		}
		m.s.Chat = append(m.s.Chat, state.Message{File: m.file, Who: who, Text: c.c.Text, At: time.Now().UnixMilli()})
		m.save()
		m.talkOpen = true
		m.msg = who + " answered"
		cmd = m.redraw()
	case "close":
		m.closeFile()
		m.openFind()
		cmd = m.find.scan()
	case "hide":
		if m.opt.Popup {
			m.quitting = true
			r.Status = m.status()
			c.reply <- r
			return m, tea.Quit
		}
		m.msg = "darshan runs in this pane: q leaves it"
	case "status":
	default:
		r = state.Response{Error: "unknown command " + c.c.Cmd}
	}
	r.Status = m.status()
	c.reply <- r
	return m, cmd
}

func (m *Model) closeFile() {
	if m.file != "" {
		m.s.Pages[m.file] = m.place()
	}
	m.g.forget(viewID1, viewID2)
	m.file, m.kind, m.img, m.lines, m.plain = "", "", nil, nil, nil
	m.s.File, m.s.Page, m.s.Agent = "", 0, state.Agent{}
	m.talkOpen = false
	_ = state.Save(m.s)
}

// ---- keys

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	switch m.mode {
	case modeHelp:
		m.mode = modeRead
		return m, nil
	case modeFind:
		return m.find.key(&m, k)
	case modeSearch, modeCommand, modeTalk:
		return m.typing(k)
	}
	m.msg = ""
	// a number before a motion repeats it
	if len(s) == 1 && s[0] >= '0' && s[0] <= '9' && (s != "0" || m.count != "") {
		m.count += s
		m.pendingG = false
		return m, nil
	}
	n := 1
	counted := m.count != ""
	if counted {
		n, _ = strconv.Atoi(m.count)
		m.count = ""
	}
	if m.pendingG {
		m.pendingG = false
		if s == "g" {
			return m, m.goTo(1, true)
		}
	}
	if m.file == "" && !strings.Contains(" q f o ctrl+p ? ctrl+c ", " "+s+" ") {
		return m, nil
	}
	switch s {
	case "q", "ctrl+c":
		return m.quit()
	case "v":
		if m.opt.Popup {
			return m.quit()
		}
	case "x":
		m.closeFile()
		m.openFind()
		return m, m.find.scan()
	case "?":
		m.mode = modeHelp
	case "f", "o", "ctrl+p":
		m.openFind()
		return m, tea.Batch(m.find.scan(), m.find.preview(&m))
	case "z":
		return m.zen()
	case "a":
		if m.s.Agent == (state.Agent{}) {
			m.s.Agent = state.Load().Agent
		}
		if m.s.Agent == (state.Agent{}) {
			m.msg = "no agent opened this file: ask one to show it with darshan's view_open"
			return m, nil
		}
		m.talkOpen, m.mode, m.input = true, modeTalk, nil
		return m, m.redraw()
	case "/":
		m.mode, m.input = modeSearch, nil
	case ":":
		m.mode, m.input = modeCommand, nil
	case "n", "N":
		return m, m.nextMatch(s == "n", n)
	case "j", "down":
		return m, m.down(n)
	case "k", "up":
		return m, m.up(n)
	case "h", "left":
		return m, m.side(-n)
	case "l", "right":
		return m, m.side(n)
	case "J", "pgdown":
		return m, m.goTo(m.pos()+n, false)
	case "K", "pgup":
		return m, m.goTo(m.pos()-n, false)
	case "ctrl+d":
		return m, m.scrollBy(n, 2)
	case "ctrl+u":
		return m, m.scrollBy(-n, 2)
	case "ctrl+f", " ":
		return m, m.scrollBy(n, 1)
	case "ctrl+b":
		return m, m.scrollBy(-n, 1)
	case "g":
		if counted {
			return m, m.goTo(n, true)
		}
		m.pendingG = true
	case "home":
		return m, m.goTo(1, true)
	case "G", "end":
		if counted {
			return m, m.goTo(n, true)
		}
		return m, m.goTo(m.lastPos(), true)
	case "+":
		m.zoom = min(m.zoom*1.25, 8)
		return m, m.redraw()
	case "-":
		m.zoom = max(m.zoom/1.25, 0.25)
		return m, m.redraw()
	case "=":
		m.zoom, m.fitWidth, m.panX, m.panY = 1, false, 0, 0
		return m, m.redraw()
	case "w":
		m.fitWidth, m.zoom, m.panX, m.panY = !m.fitWidth, 1, 0, 0
		return m, m.redraw()
	case "i":
		m.s.Invert = !m.s.Invert
		m.save()
		return m, m.redraw()
	case "r":
		m.stamp = ""
		return m, nil
	case "y":
		m.msg = copyText(m.file)
	case "[", "]":
		return m, m.sibling(s == "]")
	}
	return m, nil
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	m.save()
	m.quitting = true
	return m, tea.Quit
}

// zen: nothing but the page. A popup comes back at full size (or normal).
func (m Model) zen() (tea.Model, tea.Cmd) {
	m.s.Zen = !m.s.Zen
	m.save()
	if m.opt.Popup && m.opt.Exe != "" {
		m.quitting, m.reopen = true, true
		return m, tea.Quit
	}
	if m.kind == doc.Markdown {
		m.renderMarkdown()
	}
	return m, m.redraw()
}

// Reopen says the popup must come back (zen changed its size).
func (m Model) Reopen() bool { return m.reopen }

func (m Model) pos() int {
	if m.kind == doc.Markdown {
		return m.scroll + 1
	}
	return m.page
}

func (m Model) lastPos() int {
	if m.kind == doc.Markdown {
		return len(m.lines)
	}
	return m.pages
}

// goTo moves to a page (or Markdown line).
func (m *Model) goTo(n int, top bool) tea.Cmd {
	n = max(1, min(n, m.lastPos()))
	if m.kind == doc.Markdown {
		_, rows := m.area()
		m.scroll = max(0, min(n-1, len(m.lines)-rows))
		m.save()
		return nil
	}
	if n == m.page {
		return nil
	}
	m.page, m.panY = n, 0
	if !top {
		m.panX = m.last.PanX
	}
	m.save()
	return m.redraw()
}

func (m *Model) canPanY() (cur, most, view int) {
	return m.panY, max(0, m.last.TargetH-m.last.ViewRows*m.g.ch), m.last.ViewRows * m.g.ch
}

func (m *Model) down(n int) tea.Cmd {
	if m.kind == doc.Markdown {
		_, rows := m.area()
		m.scroll = max(0, min(m.scroll+n, len(m.lines)-rows))
		m.save()
		return nil
	}
	cur, most, view := m.canPanY()
	if most > 0 && cur < most {
		m.panY = min(cur+n*view/6, most)
		return m.redraw()
	}
	if m.page < m.pages {
		m.page, m.panY = min(m.page+n, m.pages), 0
		m.save()
		return m.redraw()
	}
	return nil
}

func (m *Model) up(n int) tea.Cmd {
	if m.kind == doc.Markdown {
		m.scroll = max(0, m.scroll-n)
		m.save()
		return nil
	}
	cur, most, view := m.canPanY()
	if most > 0 && cur > 0 {
		m.panY = max(cur-n*view/6, 0)
		return m.redraw()
	}
	if m.page > 1 {
		m.page = max(m.page-n, 1)
		m.panY = 1 << 30 // the bottom of the previous page, as when scrolling
		if most == 0 {
			m.panY = 0
		}
		m.save()
		return m.redraw()
	}
	return nil
}

func (m *Model) side(n int) tea.Cmd {
	if m.kind == doc.Markdown {
		return nil
	}
	vw := m.last.ViewCols * m.g.cw
	if m.last.TargetW > vw {
		m.panX = max(0, min(m.panX+n*vw/6, m.last.TargetW-vw))
		return m.redraw()
	}
	if m.kind == doc.Image {
		return m.sibling(n > 0)
	}
	return m.goTo(m.page+n, true)
}

// scrollBy moves n screens, a div'th of one at a time.
func (m *Model) scrollBy(n, div int) tea.Cmd {
	if m.kind == doc.Markdown {
		_, rows := m.area()
		m.scroll = max(0, min(m.scroll+n*rows/div, len(m.lines)-rows))
		m.save()
		return nil
	}
	cur, most, view := m.canPanY()
	if most == 0 {
		return m.goTo(m.page+n, true)
	}
	next := cur + n*view/div
	switch {
	case next > most && cur == most:
		return m.goTo(m.page+1, true)
	case next < 0 && cur == 0 && m.page > 1:
		m.page--
		m.panY = 1 << 30
		return m.redraw()
	}
	m.panY = max(0, min(next, most))
	return m.redraw()
}

func (m *Model) sibling(next bool) tea.Cmd {
	ents, err := os.ReadDir(filepath.Dir(m.file))
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && doc.KindOf(e.Name()) != "" {
			files = append(files, filepath.Join(filepath.Dir(m.file), e.Name()))
		}
	}
	sort.Strings(files)
	for i, f := range files {
		if f == m.file {
			if next {
				i++
			} else {
				i--
			}
			if i < 0 || i >= len(files) {
				m.msg = "no more files in this folder"
				return nil
			}
			if err := m.load(files[i], 0); err != nil {
				m.msg = err.Error()
				return nil
			}
			return m.redraw()
		}
	}
	return nil
}

// ---- search in the document

func (m *Model) runSearch(q string) tea.Cmd {
	m.query, m.matches, m.matchAt = q, nil, 0
	if q == "" {
		return nil
	}
	lq := strings.ToLower(q)
	switch m.kind {
	case doc.Markdown:
		for i, l := range m.plain {
			if strings.Contains(strings.ToLower(l), lq) {
				m.matches = append(m.matches, i+1)
			}
		}
	case doc.PDF:
		if m.pageText == nil {
			m.pageText, _ = doc.PDFText(m.file)
		}
		for i, t := range m.pageText {
			if strings.Contains(strings.ToLower(t), lq) {
				m.matches = append(m.matches, i+1)
			}
		}
	default:
		m.msg = "a picture has no text to search"
		return nil
	}
	if len(m.matches) == 0 {
		m.msg = "no match for " + q
		return nil
	}
	// the first match from here on
	for i, p := range m.matches {
		if p >= m.pos() {
			m.matchAt = i
			break
		}
	}
	return m.showMatch()
}

func (m *Model) showMatch() tea.Cmd {
	where := "line"
	if m.kind == doc.PDF {
		where = "page"
	}
	p := m.matches[m.matchAt]
	m.msg = fmt.Sprintf("%q: %d of %d, %s %d", m.query, m.matchAt+1, len(m.matches), where, p)
	if m.kind == doc.Markdown {
		_, rows := m.area()
		m.scroll = max(0, min(p-1-rows/3, len(m.lines)-rows))
		return nil
	}
	return m.goTo(p, true)
}

func (m *Model) nextMatch(forward bool, n int) tea.Cmd {
	if len(m.matches) == 0 {
		if m.query == "" {
			m.msg = "/ searches the text"
		} else {
			m.msg = "no match for " + m.query
		}
		return nil
	}
	d := n
	if !forward {
		d = -n
	}
	m.matchAt = ((m.matchAt+d)%len(m.matches) + len(m.matches)) % len(m.matches)
	return m.showMatch()
}

// ---- the input line (/, : and the talk)

func (m Model) typing(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "ctrl+c":
		was := m.mode
		m.mode, m.input = modeRead, nil
		if was == modeTalk {
			m.talkOpen = false
			return m, m.redraw()
		}
		return m, nil
	case "enter":
		text := strings.TrimSpace(string(m.input))
		was := m.mode
		m.input = nil
		switch was {
		case modeSearch:
			m.mode = modeRead
			return m, m.runSearch(text)
		case modeCommand:
			m.mode = modeRead
			return m.runCommand(text)
		case modeTalk:
			if text == "" {
				return m, nil
			}
			return m, m.ask(text)
		}
	case "backspace":
		if n := len(m.input); n > 0 {
			m.input = m.input[:n-1]
		} else if m.mode != modeTalk {
			m.mode = modeRead
		}
	case "ctrl+u":
		m.input = nil
	case "ctrl+w":
		s := strings.TrimRight(string(m.input), " ")
		m.input = []rune(s[:strings.LastIndexByte(s, ' ')+1])
	default:
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			m.input = append(m.input, k.Runes...)
		}
	}
	return m, nil
}

func (m Model) runCommand(c string) (tea.Model, tea.Cmd) {
	if n, err := strconv.Atoi(c); err == nil {
		return m, m.goTo(n, true)
	}
	f := strings.Fields(c)
	if len(f) == 0 {
		return m, nil
	}
	switch f[0] {
	case "q", "quit":
		return m.quit()
	case "bd", "close":
		m.closeFile()
		m.openFind()
		return m, m.find.scan()
	case "e", "o", "open":
		if len(f) > 1 {
			p := strings.TrimSpace(strings.TrimPrefix(c, f[0]))
			if strings.HasPrefix(p, "~/") {
				h, _ := os.UserHomeDir()
				p = filepath.Join(h, p[2:])
			}
			if err := m.load(p, 0); err != nil {
				m.msg = err.Error()
				return m, nil
			}
			return m, m.redraw()
		}
	}
	m.msg = "not a command: " + c + " (a page number, q, bd, e FILE)"
	return m, nil
}

// ---- the talk with the agent

type delivered struct{ text string }

// visibleText is what's on screen, as text: for the agent's context.
func (m Model) visibleText() string {
	const most = 2500
	var t string
	switch m.kind {
	case doc.Markdown:
		_, rows := m.area()
		t = strings.Join(m.plain[min(m.scroll, len(m.plain)):min(m.scroll+rows, len(m.plain))], "\n")
	case doc.PDF:
		pages := m.pageText
		if pages == nil {
			pages, _ = doc.PDFText(m.file)
		}
		if m.page-1 < len(pages) {
			t = pages[m.page-1]
		}
	}
	t = strings.TrimSpace(t)
	if len(t) > most {
		t = t[:most] + "\n[…]"
	}
	return t
}

func (m *Model) ask(text string) tea.Cmd {
	q := state.Question{File: m.file, Kind: m.kind, Page: m.place(), Text: text, Context: m.visibleText(), At: time.Now().UnixMilli()}
	if m.kind == doc.PDF {
		q.Pages = m.pages
	}
	m.s.Chat = append(m.s.Chat, state.Message{File: m.file, Who: "you", Text: text, At: q.At})
	m.save()
	m.msg = "sending…"
	agent := m.s.Agent
	return func() tea.Msg {
		res, err := state.Deliver(q, agent)
		if err != nil {
			return delivered{"couldn't send: " + err.Error()}
		}
		return delivered{res}
	}
}

// ---- drawing

// View draws the screen.
func (m Model) View() string {
	if m.w == 0 || m.quitting {
		return ""
	}
	if m.mode == modeFind && m.find != nil {
		return m.find.view(&m)
	}
	var rows []string
	if !m.s.Zen {
		rows = append(rows, m.top())
	}
	cols, h := m.area()
	body := m.body(cols, h)
	if m.talkOpen {
		talk := strings.Split(m.talkView(m.talkWidth(), h), "\n")
		for i := range body {
			if i < len(talk) {
				body[i] += talk[i]
			}
		}
	}
	rows = append(rows, body...)
	if !m.s.Zen {
		rows = append(rows, m.footer())
	} else if m.msg != "" || m.mode == modeSearch || m.mode == modeCommand {
		rows[len(rows)-1] = m.footer() // a moment's message, over the page's last row
	}
	out := strings.Join(rows, "\n")
	if m.mode == modeHelp {
		box := m.helpBox()
		out = ui.Overlay(out, box, m.w, max(0, (m.h-len(box))/2))
	}
	return out
}

func (m Model) top() string {
	st := m.st
	name, icon := "darshan", ""
	if m.file != "" {
		name = filepath.Base(m.file)
		icon = map[string]string{doc.PDF: "󰈦 ", doc.Image: "󰋩 ", doc.Markdown: "󰍔 "}[m.kind]
	}
	left := " " + st.Pill(icon+name, st.T.Accent)
	var right []string
	if m.kind == doc.PDF {
		right = append(right, fmt.Sprintf("page %d of %d", m.page, m.pages))
	}
	if m.kind == doc.Markdown && len(m.lines) > 0 {
		_, rows := m.area()
		right = append(right, fmt.Sprintf("%d%%", min(100, (m.scroll+rows)*100/len(m.lines))))
	}
	if m.kind != doc.Markdown && m.file != "" {
		z := fmt.Sprintf("%d%%", int(m.zoom*100))
		if m.fitWidth {
			z = "width " + z
		}
		right = append(right, z)
	}
	if m.s.Invert {
		right = append(right, "dark")
	}
	if m.s.Agent.Name != "" {
		right = append(right, m.s.Agent.Name)
	}
	return ui.Row(left, st.Muted.Render(strings.Join(right, " · "))+" ", m.w)
}

func (m Model) footer() string {
	st := m.st
	switch m.mode {
	case modeSearch:
		return ui.Fit(" "+st.Pill("search", st.T.Accent)+" "+st.Text.Render(string(m.input))+"▏", m.w)
	case modeCommand:
		return ui.Fit(" "+st.Pill("command", st.T.Accent)+" :"+st.Text.Render(string(m.input))+"▏", m.w)
	case modeTalk:
		return st.Footer("ask", [][2]string{{"⏎", "send"}, {"esc", "close"}}, m.msg, m.w)
	}
	keys := [][2]string{{"j/k", "move"}, {"/", "search"}, {"f", "find file"}, {"+/-", "zoom"}, {"i", "dark"}, {"z", "zen"}, {"a", "ask"}, {"q", "hide"}, {"x", "close"}, {"?", "keys"}}
	return st.Footer("read", keys, m.msg, m.w)
}

// body draws the page, picture or text in the room, centred.
func (m Model) body(cols, h int) []string {
	st := m.st
	blank := strings.Repeat(" ", cols)
	out := make([]string, 0, h)
	center := func(l string, width int) string {
		left := max(0, (cols-width)/2)
		return strings.Repeat(" ", left) + l + strings.Repeat(" ", max(0, cols-left-width))
	}
	var lines []string
	width := m.imgCols
	switch {
	case m.loadErr != "":
		lines, width = []string{st.Red.Render(m.loadErr)}, lipgloss.Width(m.loadErr)
	case m.file == "":
		lines, width = []string{st.Muted.Render("No file open: f finds one")}, 25
	case m.kind == doc.Markdown:
		width = min(cols-2, 100)
		end := min(m.scroll+h, len(m.lines))
		hit := map[int]bool{}
		for _, p := range m.matches {
			hit[p-1] = true
		}
		for i := min(m.scroll, end); i < end; i++ {
			l := m.lines[i]
			w := ansi.StringWidth(l)
			if hit[i] {
				l = st.Accent.Render("▌") + ansi.Truncate(l, width-1, "")
				w = min(w+1, width)
			}
			out = append(out, center(l+strings.Repeat(" ", max(0, width-w)), width))
		}
		for len(out) < h {
			out = append(out, blank)
		}
		return out
	case m.img == nil:
		lines, width = []string{st.Muted.Render("rendering…")}, 10
	default:
		lines = m.img
	}
	pad := max(0, (h-len(lines))/2)
	for range pad {
		out = append(out, blank)
	}
	for _, l := range lines {
		if len(out) < h {
			out = append(out, center(l, width))
		}
	}
	for len(out) < h {
		out = append(out, blank)
	}
	return out
}

func (m Model) talkView(w, h int) string {
	st := m.st
	inner := w - 4
	var lines []string
	for _, c := range m.s.Chat {
		if c.File != m.file {
			continue
		}
		who := st.Accent.Bold(true).Render(c.Who)
		if c.Who == "you" {
			who = st.Sub.Bold(true).Render("you")
		}
		lines = append(lines, who)
		for _, l := range ui.Wrap(c.Text, inner) {
			lines = append(lines, st.Text.Render(l))
		}
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		lines = append(lines, st.Muted.Render("Ask "+firstOr(m.s.Agent.Name, "the agent")+" about this."), st.Muted.Render("It gets your question"), st.Muted.Render("and what's on screen."), "")
	}
	input := []string{st.Muted.Render(strings.Repeat("─", inner))}
	in := ui.Wrap(string(m.input), inner)
	if len(in) == 0 {
		in = []string{""}
	}
	for i, l := range in {
		if i == len(in)-1 && m.mode == modeTalk {
			l += "▏"
		}
		input = append(input, st.Text.Render(l))
	}
	room := max(1, h-2-len(input))
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	for len(lines) < room {
		lines = append(lines, "")
	}
	return st.Box(w, h, "talk · "+firstOr(m.s.Agent.Name, "agent"), append(lines, input...), m.mode == modeTalk, st.T.Accent)
}

func firstOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (m Model) helpBox() []string {
	st := m.st
	var lines []string
	where := ""
	for _, k := range Keys {
		if k.Where != where {
			if where != "" {
				lines = append(lines, "")
			}
			where = k.Where
			lines = append(lines, st.Heading(where))
		}
		lines = append(lines, st.Accent.Render(ui.Fit(k.Keys, 22))+st.Text.Render(k.Desc))
	}
	w := min(m.w-4, 110)
	if len(lines) > m.h-4 {
		lines = lines[:max(1, m.h-4)]
	}
	return st.Popup("keys", lines, w, st.T.Accent)
}

func copyText(s string) string {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		if _, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Stdin = strings.NewReader(s)
			if cmd.Run() == nil {
				return "copied the path"
			}
		}
	}
	return "copying needs wl-copy or xclip"
}
