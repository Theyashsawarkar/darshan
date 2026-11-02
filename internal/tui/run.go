package tui

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Theyashsawarkar/darshan/internal/state"
)

// Run runs the screen until it's closed, serving commands from other
// darshan processes meanwhile.
func Run(o Options) error {
	m := New(o)
	p := tea.NewProgram(m, tea.WithAltScreen())
	SetProgram(p)
	l, err := state.Listen(func(c state.Command) state.Response {
		reply := make(chan state.Response, 1)
		p.Send(cmdMsg{c, reply})
		select {
		case r := <-reply:
			return r
		case <-time.After(4 * time.Second):
			return state.Response{Error: "the screen didn't answer"}
		}
	})
	if err != nil {
		return err
	}
	defer l.Close()
	final, err := p.Run()
	if err != nil {
		return err
	}
	fm := final.(Model)
	fm.g.forget(viewID1, viewID2, previewID1, previewID2)
	if fm.png != "" {
		_ = os.Remove(fm.png)
	}
	if fm.Reopen() {
		// zen changed the popup's size: it comes back once this one is gone
		cmd := fmt.Sprintf("sleep 0.15; '%s' popup --open --client '%s'", o.Exe, o.Client)
		_ = exec.Command("tmux", "run-shell", "-b", cmd).Run()
	}
	return nil
}
