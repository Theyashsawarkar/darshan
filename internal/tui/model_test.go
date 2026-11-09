package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		case "ctrl+f":
			msg = tea.KeyMsg{Type: tea.KeyCtrlF}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func mdModel(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DARSHAN_HOME", filepath.Join(dir, "home"))
	t.Setenv("DARSHAN_GRAPHICS", "blocks")
	var b strings.Builder
	for i := range 100 {
		b.WriteString("line number ")
		b.WriteString(strings.Repeat("x", i%5))
		b.WriteString("\n\n")
	}
	b.WriteString("needle here\n")
	p := filepath.Join(dir, "a.md")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{File: p})
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return nm.(Model), p
}

func TestVimMotions(t *testing.T) {
	m, _ := mdModel(t)
	m = press(t, m, "j", "j", "j")
	if m.scroll != 3 {
		t.Fatalf("jjj: scroll %d", m.scroll)
	}
	m = press(t, m, "k")
	if m.scroll != 2 {
		t.Fatalf("k: scroll %d", m.scroll)
	}
	m = press(t, m, "5", "j")
	if m.scroll != 7 {
		t.Fatalf("5j: scroll %d", m.scroll)
	}
	m = press(t, m, "G")
	if m.scroll == 7 || m.scroll == 0 {
		t.Fatalf("G: scroll %d", m.scroll)
	}
	m = press(t, m, "g", "g")
	if m.scroll != 0 {
		t.Fatalf("gg: scroll %d", m.scroll)
	}
	m = press(t, m, "ctrl+d")
	if m.scroll == 0 {
		t.Fatal("ctrl+d didn't scroll")
	}
}

func TestSearchInMarkdown(t *testing.T) {
	m, _ := mdModel(t)
	m = press(t, m, "/", "n", "e", "e", "d", "l", "e", "enter")
	if len(m.matches) != 1 || m.mode != modeRead || !strings.Contains(m.msg, "1 of 1") {
		t.Fatalf("search: %v %q", m.matches, m.msg)
	}
	if m.scroll == 0 {
		t.Fatal("search didn't move to the match")
	}
}

func TestLastFileStaysOpen(t *testing.T) {
	m, p := mdModel(t)
	m = press(t, m, "j", "j", "j", "j", "q")
	if !m.quitting {
		t.Fatal("q should leave")
	}
	// next time: the same file, the same place
	n := New(Options{})
	if n.file != p {
		t.Fatalf("reopened on %q, want %q", n.file, p)
	}
	nm, _ := n.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if got := nm.(Model).scroll; got != 4 {
		t.Fatalf("reopened at line %d", got)
	}
	// Space b d closes it for good
	n = press(t, nm.(Model), " ", "b", "d")
	if again := New(Options{}); again.file != "" {
		t.Fatalf("Space b d left %q open", again.file)
	}
}

func TestNoAgentNoTalk(t *testing.T) {
	m, _ := mdModel(t)
	m = press(t, m, " ", "k")
	if m.mode == modeTalk || !strings.Contains(m.msg, "no agent") {
		t.Fatalf("Space k without an agent: mode %d %q", m.mode, m.msg)
	}
}

func TestSpaceIsTheLeader(t *testing.T) {
	m, _ := mdModel(t)
	m = press(t, m, " ")
	if m.scroll != 0 || !m.eng.Waiting() {
		t.Fatalf("Space paged (scroll %d) or isn't waiting", m.scroll)
	}
	m = press(t, m, "esc", "ctrl+f")
	if m.scroll == 0 {
		t.Fatal("Ctrl+f didn't page down")
	}
	m = press(t, m, "a")
	if !strings.Contains(m.msg, "moved to Space k") {
		t.Fatalf("a: %q", m.msg)
	}
	m = press(t, m, " ", " ")
	if m.mode != modePalette {
		t.Fatalf("Space Space: mode %d", m.mode)
	}
}

func TestPopupKeyCloses(t *testing.T) {
	m, _ := mdModel(t)
	m.opt.Popup, m.opt.PopupKey = true, "v"
	m = press(t, m, "v")
	if !m.quitting {
		t.Fatal("the popup key didn't close the popup")
	}
}

func TestZenHidesEverything(t *testing.T) {
	m, _ := mdModel(t)
	if !strings.Contains(m.View(), "a.md") {
		t.Fatal("no file name in the top line")
	}
	m = press(t, m, "z")
	if strings.Contains(m.View(), "a.md") {
		t.Fatal("zen still shows the top line")
	}
}

// Keys typed faster than the terminal is read come as one message ("5j",
// "gg", "  "): each must still count as its own key.
func TestFastKeysArriveTogether(t *testing.T) {
	m, _ := mdModel(t)
	burst := func(s string) {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = nm.(Model)
	}
	burst("5j")
	if m.scroll != 5 {
		t.Fatalf("5j in one message: scroll %d", m.scroll)
	}
	burst("gg")
	if m.scroll != 0 {
		t.Fatalf("gg in one message: scroll %d", m.scroll)
	}
	burst("  ")
	if m.mode != modePalette {
		t.Fatalf("Space Space in one message: mode %d", m.mode)
	}
}
