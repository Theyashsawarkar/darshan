package tui

import (
	"fmt"
	"strings"
)

// Key is one key of the screen.
type Key struct {
	Keys, Desc, Where string
}

// Keys is every key, in the order the ? sheet lists them. The README's key
// table and scripts/keybindings (Vayu's keybinding search) come from it:
// after a change run `go test ./internal/tui -update`.
var Keys = []Key{
	{"j / k", "Move down / up (next / previous page once the page is all shown)", "reading"},
	{"h / l", "Move left / right when zoomed (else previous / next page)", "reading"},
	{"J / K", "Move to the next / previous page", "reading"},
	{"Ctrl+d / Ctrl+u", "Scroll half a screen down / up", "reading"},
	{"Ctrl+f / Ctrl+b", "Scroll a screen down / up", "reading"},
	{"g g / G", "Jump to the first / last page", "reading"},
	{"N G / : N", "Jump to page (or line) N; a number before j, k, J or K repeats it", "reading"},
	{"/", "Search the document's text", "reading"},
	{"n / N", "Jump to the next / previous match", "reading"},
	{"+ / -", "Grow / shrink the zoom", "reading"},
	{"=", "Reset to fit the whole page", "reading"},
	{"w", "Toggle fitting the page's width", "reading"},
	{"i", "Invert the colours of pages and pictures (dark mode)", "reading"},
	{"[ / ]", "Switch to the previous / next file in the folder", "reading"},
	{"f / o / Ctrl+p", "Search for a file on the whole system (with a preview)", "reading"},
	{"a", "Open the talk with the agent that opened this file, to ask it something", "reading"},
	{"z", "Toggle zen mode: the full screen, nothing but the page", "reading"},
	{"r", "Reload the file (it also reloads by itself when it changes)", "reading"},
	{"y", "Copy the file's path", "reading"},
	{"q / v", "Close the popup; the file stays open for next time", "reading"},
	{"x", "Close the file", "reading"},
	{"?", "Show every key", "reading"},
	{"Enter", "Open the picked file", "search"},
	{"Up / Down / Ctrl+j / Ctrl+k", "Move through the results while typing", "search"},
	{"Esc", "Leave typing, then j, k, g g, G and Ctrl+d / Ctrl+u move (i types again; Esc again closes)", "search"},
	{"Tab / Shift+Tab", "Switch between all files, PDFs, images and Markdown", "search"},
	{"Enter", "Send your question, with what's on screen, to the agent", "talk"},
	{"Esc", "Close the talk", "talk"},
}

// KeybindingsFile is scripts/keybindings, for Vayu's keybinding search.
func KeybindingsFile() string {
	var b strings.Builder
	b.WriteString("# Keys inside the darshan screen, for Vayu's keybinding search\n")
	b.WriteString("# (keybind-search.py reads ~/.tmux/plugins/*/scripts). Generated from\n")
	b.WriteString("# internal/tui/keys.go by `go test ./internal/tui -update`; don't edit.\n")
	// marker is kept apart from the rest of the line: the binary holds this
	// format string, and the keybinding search scans plugins' bin/ folders.
	marker := strings.Join([]string{"#", "keybind"}, " ")
	for _, k := range Keys {
		fmt.Fprintf(&b, "%s: Tmux/darshan %s | %s | %s\n", marker, k.Where, k.Keys, k.Desc)
	}
	return b.String()
}
