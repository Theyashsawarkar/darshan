// darshan: PDFs, images and Markdown in the terminal, with vim keys, a search
// over the whole system, and a line to the AI agent that opened the file.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Theyashsawarkar/darshan/internal/doc"
	"github.com/Theyashsawarkar/darshan/internal/index"
	"github.com/Theyashsawarkar/darshan/internal/mcpserver"
	"github.com/Theyashsawarkar/darshan/internal/state"
	"github.com/Theyashsawarkar/darshan/internal/tui"
)

// Version is darshan's version.
const Version = "0.1.0"

const usage = `darshan: PDFs, images and Markdown in the terminal.

  darshan [FILE] [-p N]    the screen, on FILE or what you left open (? lists every key)
  darshan open FILE [-p N] show FILE from anywhere: the running screen, a tmux
                           popup, or a window of its own from the desktop
  darshan popup            open the tmux popup, or close it when it's open
  darshan search WORDS     find files by name, across the system: one path a line
  darshan status           what's open
  darshan close            close the open file
  darshan mcp              the MCP server, for AI agents
  darshan doctor           check what it needs
  darshan --version

Keys, the tmux plugin and its options: https://github.com/Theyashsawarkar/darshan
`

func die(err error) {
	fmt.Fprintln(os.Stderr, "darshan:", err)
	os.Exit(1)
}

func main() {
	fs := flag.NewFlagSet("darshan", flag.ExitOnError)
	popup := fs.Bool("popup", false, "")
	client := fs.String("client", "", "")
	page := fs.Int("p", 0, "")
	openNow := fs.Bool("open", false, "")
	kind := fs.String("kind", "", "")
	version := fs.Bool("version", false, "")
	fs.Usage = func() { fmt.Print(usage) }

	// flags may come after the file
	var pos, flags []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if (a == "-p" || a == "--client" || a == "--kind" || a == "-client" || a == "-kind") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		pos = append(pos, a)
	}
	_ = fs.Parse(flags)
	exe, _ := os.Executable()

	cmd := ""
	if len(pos) > 0 {
		switch pos[0] {
		case "open", "popup", "search", "status", "close", "mcp", "doctor", "help", "keybindings":
			cmd, pos = pos[0], pos[1:]
		}
	}
	switch {
	case *version:
		fmt.Println("darshan", Version)
	case cmd == "help":
		fmt.Print(usage)
	case cmd == "keybindings": // scripts/keybindings, for Vayu's keybinding search
		fmt.Print(tui.KeybindingsFile())
	case cmd == "mcp":
		if err := mcpserver.Run(context.Background(), Version); err != nil {
			die(err)
		}
	case cmd == "open":
		if len(pos) != 1 {
			die(fmt.Errorf("open takes one file"))
		}
		abs, err := filepath.Abs(pos[0])
		if err != nil {
			die(err)
		}
		if err := state.Show(abs, *page, state.Agent{}, "", *client); err != nil {
			die(err)
		}
	case cmd == "popup":
		if !*openNow {
			if _, err := state.Send(state.Command{Cmd: "hide"}); err == nil {
				return
			}
		}
		if err := state.OpenPopup(exe, state.Client(*client, os.Getenv("TMUX_PANE")), state.Load().Zen); err != nil {
			die(err)
		}
	case cmd == "search":
		paths := index.Cached()
		if len(paths) == 0 {
			var err error
			if paths, err = index.Refresh(context.Background(), index.LoadConfig(), nil); err != nil {
				die(err)
			}
		}
		for _, p := range index.Search(paths, strings.Join(pos, " "), *kind, state.Load().Recent, 50) {
			fmt.Println(p)
		}
	case cmd == "status":
		r, err := state.Send(state.Command{Cmd: "status"})
		if err != nil {
			s := state.Load()
			if s.File == "" {
				fmt.Println("nothing open")
				return
			}
			fmt.Printf("%s\thidden\tpage %d\n", s.File, s.Page)
			return
		}
		fmt.Printf("%s\tshowing\tpage %d of %d\n", r.Status.File, r.Status.Page, r.Status.Pages)
	case cmd == "close":
		if _, err := state.Send(state.Command{Cmd: "close"}); err != nil {
			s := state.Load()
			s.File, s.Page, s.Agent = "", 0, state.Agent{}
			if err := state.Save(s); err != nil {
				die(err)
			}
		}
	case cmd == "doctor":
		doctor()
	default:
		file := ""
		if len(pos) > 1 {
			die(fmt.Errorf("one file at a time"))
		}
		if len(pos) == 1 {
			file = pos[0]
		}
		// already running: this file goes there
		if file != "" {
			abs, _ := filepath.Abs(file)
			if _, err := state.Send(state.Command{Cmd: "open", Path: abs, Page: *page}); err == nil {
				return
			}
		}
		if err := tui.Run(tui.Options{File: file, Page: *page, Popup: *popup, Client: *client, Exe: exe}); err != nil {
			die(err)
		}
	}
}

func doctor() {
	bad := false
	for _, t := range []struct{ bin, why string }{
		{"pdftoppm", "draws PDF pages (poppler)"}, {"pdfinfo", "counts PDF pages (poppler)"}, {"pdftotext", "searches PDFs (poppler)"},
		{"fd", "finds files across the system"}, {"tmux", "the popup (optional)"}, {"wl-copy", "copies paths (optional; or xclip)"},
	} {
		if p, err := exec.LookPath(t.bin); err == nil {
			fmt.Printf("✓ %-10s %s\n", t.bin, p)
		} else {
			fmt.Printf("✗ %-10s %s: not found\n", t.bin, t.why)
			if !strings.Contains(t.why, "optional") {
				bad = true
			}
		}
	}
	t := os.Getenv("TERM")
	fmt.Printf("  terminal   %s (real pictures need kitty's graphics protocol; others get coloured blocks)\n", t)
	fmt.Printf("  shows      %s\n", strings.Join(extList(), " "))
	fmt.Printf("  searches   %s\n", strings.Join(index.Roots(index.LoadConfig()), " "))
	if bad {
		os.Exit(1)
	}
}

func extList() []string {
	var out []string
	for e := range doc.Exts {
		out = append(out, e)
	}
	return out
}
