package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Theyashsawarkar/darshan/internal/doc"
	"github.com/Theyashsawarkar/darshan/internal/index"
)

// finder searches the whole system, with a live preview of the pick.
type finder struct {
	query  []rune
	all    []string
	hits   []string
	sel    int
	kind   int
	normal bool
	gg     bool

	scanning bool
	cancel   context.CancelFunc

	seq   int // the newest pick the preview waits for
	pGen  int
	pPath string
	pImg  []string
	pCols int
	pText []string
	pErr  string
	pID   uint32
	pPNG  string
	pInfo string
}

var kinds = []struct{ name, kind string }{{"All", ""}, {"PDF", doc.PDF}, {"Images", doc.Image}, {"Markdown", doc.Markdown}}

type (
	batchMsg   []string
	scanDone   struct{}
	previewDue int
)

func (m *Model) openFind() {
	m.mode = modeFind
	m.find = &finder{all: index.Cached(), pID: previewID1}
	m.find.search(m)
}

func (m *Model) closeFind() {
	if m.find == nil {
		return
	}
	if m.find.cancel != nil {
		m.find.cancel()
	}
	m.g.forget(previewID1, previewID2)
	if m.find.pPNG != "" {
		_ = os.Remove(m.find.pPNG)
	}
	m.find = nil
	m.mode = modeRead
}

// program lets the scan report as it goes.
var program *tea.Program

// SetProgram gives the screen its running program.
func SetProgram(p *tea.Program) { program = p }

func (f *finder) scan() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.scanning = cancel, true
	f.all = nil
	f.all = index.Cached()
	return func() tea.Msg {
		fresh := []string{}
		_, _ = index.Refresh(ctx, index.LoadConfig(), func(b []string) {
			fresh = append(fresh, b...)
			if program != nil {
				program.Send(batchMsg(b))
			}
		})
		cancel()
		return scanDone{}
	}
}

func (f *finder) search(m *Model) {
	f.hits = index.Search(f.all, string(f.query), kinds[f.kind].kind, m.s.Recent, 500)
	f.sel = min(f.sel, max(0, len(f.hits)-1))
}

func (f *finder) cur() string {
	if f.sel < len(f.hits) {
		return f.hits[f.sel]
	}
	return ""
}

func (f *finder) update(m *Model, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case batchMsg:
		have := make(map[string]bool, len(f.all))
		for _, p := range f.all {
			have[p] = true
		}
		for _, p := range msg {
			if !have[p] {
				f.all = append(f.all, p)
			}
		}
		was := f.cur()
		f.search(m)
		if f.cur() != was {
			return *m, f.preview(m)
		}
	case scanDone:
		f.scanning = false
	case previewDue:
		if int(msg) == f.seq {
			return *m, f.renderPreview(m)
		}
	}
	return *m, nil
}

// preview asks for the pick's preview after a moment, so moving quickly
// through the list doesn't render every file on the way.
func (f *finder) preview(m *Model) tea.Cmd {
	f.seq++
	seq := f.seq
	return tea.Tick(110*time.Millisecond, func(time.Time) tea.Msg { return previewDue(seq) })
}

func (f *finder) previewRoom(m *Model) (cols, rows int) {
	return max(10, m.w-m.w*42/100-4), max(3, m.h-3-2-3)
}

func (f *finder) renderPreview(m *Model) tea.Cmd {
	p := f.cur()
	if p == "" {
		f.pPath, f.pImg, f.pText = "", nil, nil
		return nil
	}
	cols, rows := f.previewRoom(m)
	f.pPath, f.pErr, f.pText, f.pInfo = p, "", nil, fileInfo(p)
	switch doc.KindOf(p) {
	case doc.Markdown:
		f.pImg = nil
		lines, err := doc.RenderMarkdown(p, cols)
		if err != nil {
			f.pErr = err.Error()
			return nil
		}
		f.pText = lines
		return nil
	default:
		f.pGen++
		return renderCmd(job{Path: p, Kind: doc.KindOf(p), Page: 1, Cols: cols, Rows: rows, CW: m.g.cw, CH: m.g.ch, Zoom: 1,
			Invert: m.s.Invert, Gen: f.pGen, Preview: true, Kitty: m.g.kitty})
	}
}

func fileInfo(p string) string {
	var parts []string
	if fi, err := os.Stat(p); err == nil {
		parts = append(parts, sizeText(fi.Size()), fi.ModTime().Format("2 Jan 2006"))
	}
	if doc.KindOf(p) == doc.PDF {
		if n, err := doc.PDFPages(p); err == nil {
			parts = append(parts, fmt.Sprintf("%d pages", n))
		}
	}
	return strings.Join(parts, " · ")
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func (f *finder) gotShot(m *Model, r shot) {
	if r.Gen != f.pGen || r.Path != f.pPath {
		if r.PNG != "" {
			_ = os.Remove(r.PNG)
		}
		return
	}
	if r.Err != nil {
		f.pErr, f.pImg = r.Err.Error(), nil
		return
	}
	f.pCols = r.ViewCols
	if r.PNG == "" {
		f.pImg = r.Blocks
		return
	}
	old := f.pID
	f.pID = previewID1
	if old == previewID1 {
		f.pID = previewID2
	}
	m.g.place(f.pID, r.PNG, r.ViewCols, r.ViewRows)
	f.pImg = placeholders(f.pID, r.ViewCols, r.ViewRows)
	m.g.forget(old)
	if f.pPNG != "" {
		_ = os.Remove(f.pPNG)
	}
	f.pPNG = r.PNG
}

func (f *finder) move(m *Model, d int) tea.Cmd {
	n := max(0, min(f.sel+d, len(f.hits)-1))
	if n == f.sel {
		return nil
	}
	f.sel = n
	return f.preview(m)
}

func (f *finder) key(m *Model, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	switch s {
	case "enter":
		p := f.cur()
		if p == "" {
			return *m, nil
		}
		m.closeFind()
		if err := m.load(p, 0); err != nil {
			m.msg = err.Error()
			m.openFind()
			return *m, m.find.scan()
		}
		return *m, m.redraw()
	case "tab", "shift+tab":
		d := 1
		if s == "shift+tab" {
			d = len(kinds) - 1
		}
		f.kind = (f.kind + d) % len(kinds)
		f.sel = 0
		f.search(m)
		return *m, f.preview(m)
	case "down", "ctrl+j", "ctrl+n":
		return *m, f.move(m, 1)
	case "up", "ctrl+k", "ctrl+p":
		return *m, f.move(m, -1)
	case "pgdown":
		return *m, f.move(m, 10)
	case "pgup":
		return *m, f.move(m, -10)
	case "ctrl+c":
		return f.leave(m)
	}
	if f.normal {
		g := f.gg
		f.gg = false
		switch s {
		case "j":
			return *m, f.move(m, 1)
		case "k":
			return *m, f.move(m, -1)
		case "ctrl+d":
			return *m, f.move(m, 10)
		case "ctrl+u":
			return *m, f.move(m, -10)
		case "g":
			if g {
				return *m, f.move(m, -len(f.hits))
			}
			f.gg = true
		case "G":
			return *m, f.move(m, len(f.hits))
		case "i", "/", "a":
			f.normal = false
		case "q", "esc":
			return f.leave(m)
		}
		return *m, nil
	}
	switch s {
	case "esc":
		if len(f.query) == 0 {
			return f.leave(m)
		}
		f.normal = true
		return *m, nil
	case "backspace":
		if n := len(f.query); n > 0 {
			f.query = f.query[:n-1]
		}
	case "ctrl+u":
		f.query = nil
	case "ctrl+w":
		t := strings.TrimRight(string(f.query), " /")
		f.query = []rune(t[:strings.LastIndexAny(t, " /")+1])
	default:
		if k.Type != tea.KeyRunes && k.Type != tea.KeySpace {
			return *m, nil
		}
		f.query = append(f.query, k.Runes...)
	}
	f.sel = 0
	f.search(m)
	return *m, f.preview(m)
}

func (f *finder) leave(m *Model) (tea.Model, tea.Cmd) {
	hadFile := m.file != ""
	m.closeFind()
	if !hadFile {
		return m.quit()
	}
	return *m, m.redraw()
}

func (f *finder) view(m *Model) string {
	st := m.st
	var tabs []string
	for _, k := range kinds {
		tabs = append(tabs, k.name)
	}
	state := fmt.Sprintf("%d of %d", len(f.hits), len(f.all))
	if f.scanning {
		state += " · scanning"
	}
	rows := []string{st.Tabs(tabs, f.kind, st.Muted.Render(state)+" ", m.w)}
	cursor := "▏"
	if f.normal {
		cursor = ""
	}
	q := string(f.query)
	line := st.Accent.Render(" ") + st.Text.Render(q) + cursor
	if q == "" {
		line = st.Accent.Render(" ") + cursor + st.Muted.Render("search every PDF, image and Markdown file")
	}
	rows = append(rows, line)
	h := m.h - 3
	lw := m.w * 42 / 100
	// the list
	inner := lw - 4
	room := max(1, h-2)
	top := max(0, f.sel-room/2)
	top = min(top, max(0, len(f.hits)-room))
	var list []string
	for i := top; i < min(top+room, len(f.hits)); i++ {
		p := f.hits[i]
		name := ansi.Truncate(filepath.Base(p), max(8, inner/2), "…")
		dir := shortDir(p)
		icon := map[string]string{doc.PDF: "󰈦", doc.Image: "󰋩", doc.Markdown: "󰍔"}[doc.KindOf(p)]
		text := icon + " " + st.Text.Render(name) + "  " + st.Muted.Render(ansi.TruncateLeft(dir, max(0, ansi.StringWidth(dir)-(inner-ansi.StringWidth(name)-4)), "…"))
		if i == f.sel {
			text = st.Highlight(ansi.Strip(text), inner)
		}
		list = append(list, text)
	}
	if len(list) == 0 {
		msg := "Nothing matches."
		if f.scanning {
			msg = "Looking through the disks…"
		}
		list = []string{st.Muted.Render(msg)}
	}
	left := strings.Split(st.Box(lw, h, "files", list, true, st.T.Accent), "\n")
	// the preview
	pw := m.w - lw
	pRoomC, pRoomR := f.previewRoom(m)
	var pv []string
	switch {
	case f.pPath == "":
	case f.pErr != "":
		pv = []string{st.Red.Render(ansi.Truncate(f.pErr, pRoomC, "…"))}
	case f.pText != nil:
		pv = f.pText[:min(len(f.pText), pRoomR)]
	default:
		lpad := strings.Repeat(" ", max(0, (pRoomC-f.pCols)/2))
		for _, l := range f.pImg {
			pv = append(pv, lpad+l)
		}
	}
	if f.pInfo != "" {
		pv = append(pv, st.Muted.Render(ansi.Truncate(f.pInfo, pRoomC, "…")))
	}
	title := "preview"
	if f.pPath != "" {
		title = ansi.Truncate(filepath.Base(f.pPath), max(5, pw-8), "…")
	}
	right := strings.Split(st.Box(pw, h, title, pv, false, st.T.Accent), "\n")
	for i := range left {
		if i < len(right) {
			left[i] += right[i]
		}
	}
	rows = append(rows, left...)
	keys := [][2]string{{"⏎", "open"}, {"↑↓", "move"}, {"tab", "kind"}, {"esc", "keys: j/k"}}
	mode := "find"
	if f.normal {
		keys = [][2]string{{"j/k", "move"}, {"gg/G", "ends"}, {"⏎", "open"}, {"i", "type"}, {"tab", "kind"}, {"esc", "close"}}
		mode = "normal"
	}
	rows = append(rows, st.Footer(mode, keys, "", m.w))
	return strings.Join(rows, "\n")
}

func shortDir(p string) string {
	d := filepath.Dir(p)
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(d, h) {
		return "~" + d[len(h):]
	}
	return d
}
