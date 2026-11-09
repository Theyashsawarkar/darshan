package tui

import (
	"github.com/Theyashsawarkar/vyakarana/keymap"
	vmode "github.com/Theyashsawarkar/vyakarana/mode"
)

// Where keys work besides the page: the file finder and the talk.
const (
	inFinder = "finder"
	inTalk   = "talk"
)

var (
	typing   = []vmode.Mode{vmode.Insert}
	anyFind  = []vmode.Mode{vmode.Insert, vmode.Normal}
	command_ = []vmode.Mode{vmode.Command}
)

// Keys is every key of the screen, in the order the help sheet lists
// them, with the vim grammar every Vayu tool shares (vyakarana): NORMAL
// on the page, a Space leader, INSERT in the finder, the search and the
// talk. The README's key tables and scripts/keybindings (Vayu's keybinding
// search) come from it: after a change run `go test ./internal/tui -update`.
// The finder, the search line and the talk handle their own keys
// (find.go, typing()); their rows here document them.
var Keys = keymap.New("darshan",
	[]keymap.Binding{
		{Seq: "j", Alt: []string{"Down"}, Act: "down", Desc: "Move down (the next page once the page is all shown; 5j repeats)", Count: true},
		{Seq: "k", Alt: []string{"Up"}, Act: "up", Desc: "Move up (the previous page once the page is all shown)", Count: true},
		{Seq: "h", Alt: []string{"Left"}, Act: "left", Desc: "Move left when zoomed (else the previous page)", Count: true},
		{Seq: "l", Alt: []string{"Right"}, Act: "right", Desc: "Move right when zoomed (else the next page)", Count: true},
		{Seq: "J", Alt: []string{"PgDn"}, Act: "next-page", Desc: "Move to the next page", Count: true},
		{Seq: "K", Alt: []string{"PgUp"}, Act: "prev-page", Desc: "Move to the previous page", Count: true},
		{Seq: "Ctrl+d", Act: "half-down", Desc: "Scroll half a screen down", Count: true},
		{Seq: "Ctrl+u", Act: "half-up", Desc: "Scroll half a screen up", Count: true},
		{Seq: "Ctrl+f", Act: "page-down", Desc: "Scroll a screen down (Space did this before it became the leader)", Count: true},
		{Seq: "Ctrl+b", Act: "page-up", Desc: "Scroll a screen up", Count: true},
		{Seq: "g g", Alt: []string{"Home"}, Act: "first", Desc: "Jump to the first page (5gg: page 5)", Count: true},
		{Seq: "G", Alt: []string{"End"}, Act: "last", Desc: "Jump to the last page (5G: page 5)", Count: true},
		{Seq: ":", Alt: []string{"Space g p"}, Act: "command", Desc: "Jump to a page (or line) by number: :12 (also :q, :bd, :e FILE)"},
		{Seq: "/", Act: "search", Desc: "Search the document's text"},
		{Seq: "n", Act: "next-match", Desc: "Jump to the next match", Count: true},
		{Seq: "N", Act: "prev-match", Desc: "Jump to the previous match", Count: true},
		{Seq: "+", Act: "zoom-in", Desc: "Grow the zoom"},
		{Seq: "-", Act: "zoom-out", Desc: "Shrink the zoom"},
		{Seq: "=", Act: "zoom-reset", Desc: "Reset to fit the whole page"},
		{Seq: "[", Alt: []string{"Space b p"}, Act: "prev-file", Desc: "Switch to the previous file in the folder"},
		{Seq: "]", Alt: []string{"Space b n"}, Act: "next-file", Desc: "Switch to the next file in the folder"},
		{Seq: "Space b d", Act: "close-file", Desc: "Close the file (the popup then finds another)"},
		{Seq: "f", Alt: []string{"o", "Ctrl+p", "Space f f"}, Act: "find", Desc: "Search for a file on the whole system (with a preview)"},
		{Seq: "Space k", Act: "talk", Desc: "Open the talk with the agent that opened this file, to ask it something"},
		{Seq: "z", Alt: []string{"Space u z"}, Act: "zen", Desc: "Toggle zen mode: the full screen, nothing but the page"},
		{Seq: "Space u i", Act: "invert", Desc: "Invert the colours of pages and pictures (dark mode)"},
		{Seq: "Space u w", Act: "fit-width", Desc: "Toggle fitting the page's width"},
		{Seq: "Space o b", Act: "open-viewer", Desc: "Open the file in the desktop's own viewer (xdg-open)"},
		{Seq: "Space o e", Act: "open-editor", Desc: "Open the file in $EDITOR (Markdown)"},
		{Seq: "r", Alt: []string{"Space r"}, Act: "reload", Desc: "Reload the file (it also reloads by itself when it changes)"},
		{Seq: "y", Alt: []string{"Space y p"}, Act: "copy-path", Desc: "Copy the file's path"},
		{Seq: "?", Alt: []string{"Space ?"}, Act: "help", Desc: "Show every key"},
		{Seq: "Space Space", Act: "palette", Desc: "Search every action and run one (the palette)"},
		{Seq: "q", Alt: []string{"Space q"}, Act: "quit", Desc: "Close the popup; the file stays open for next time (the popup's own key too)"},
		{Seq: "Ctrl+c", Act: "quit", Desc: "Close the popup", Hide: true},
		{Seq: "a", Moved: "Space k"},
		{Seq: "i", Moved: "Space u i"},
		{Seq: "w", Moved: "Space u w"},
		{Seq: "x", Moved: "Space b d"},

		{Seq: "Enter", Act: "", Desc: "Open the picked file", Where: inFinder, Modes: anyFind},
		{Seq: "Ctrl+j", Alt: []string{"Down", "Ctrl+n"}, Desc: "Move to the next result while typing", Where: inFinder, Modes: typing},
		{Seq: "Ctrl+k", Alt: []string{"Up", "Ctrl+p"}, Desc: "Move to the previous result while typing", Where: inFinder, Modes: typing},
		{Seq: "Tab", Alt: []string{"Shift+Tab"}, Desc: "Switch between all files, PDFs, images and Markdown", Where: inFinder, Modes: anyFind},
		{Seq: "Esc", Desc: "Leave typing (NORMAL: j, k, g g, G, Ctrl+d / Ctrl+u move; i, a or / type again; Esc or q closes)", Where: inFinder, Modes: typing},

		{Seq: "Enter", Desc: "Send your question, with what's on screen, to the agent", Where: inTalk, Modes: typing},
		{Seq: "Esc", Desc: "Close the talk", Where: inTalk, Modes: typing},

		{Seq: "Enter", Desc: "Run the search or the : command", Modes: command_},
		{Seq: "Esc", Desc: "Cancel the search or the : command", Modes: command_},
	}).
	Group("Space b", "File").
	Where(inFinder, "The file finder").
	Where(inTalk, "The talk with the agent")
