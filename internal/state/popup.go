package state

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Client finds the tmux client to show a popup on: the one given, the one
// holding pane, else the one used last.
func Client(given, pane string) string {
	if given != "" {
		return given
	}
	if pane != "" {
		if out, err := exec.Command("tmux", "display", "-p", "-t", pane, "#{client_name}").Output(); err == nil {
			if c := strings.TrimSpace(string(out)); c != "" {
				return c
			}
		}
	}
	out, err := exec.Command("tmux", "list-clients", "-F", "#{client_activity} #{client_name}").Output()
	if err != nil {
		return ""
	}
	best, bestAt := "", ""
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		at, name, ok := strings.Cut(l, " ")
		if ok && at > bestAt {
			best, bestAt = name, at
		}
	}
	return best
}

func tmuxOpt(name, def string) string {
	out, err := exec.Command("tmux", "show-option", "-gqv", name).Output()
	if v := strings.TrimSpace(string(out)); err == nil && v != "" {
		return v
	}
	return def
}

// OpenPopup shows the screen in a tmux popup: full screen in zen mode, else
// at the plugin's size or through its launcher (@darshan-launcher).
func OpenPopup(exe, client string, zen bool) error {
	if client == "" {
		return fmt.Errorf("no tmux client to show darshan on")
	}
	run := fmt.Sprintf("'%s' --popup --client '%s'", exe, client)
	var c *exec.Cmd
	launcher := tmuxOpt("@darshan-launcher", "")
	switch {
	case zen:
		c = exec.Command("tmux", "display-popup", "-c", client, "-E", "-B", "-w", "100%", "-h", "100%", "-x", "C", "-y", "C", run)
	case launcher != "":
		exp, err := exec.Command("tmux", "display", "-p", "-c", client, launcher).Output()
		if err != nil {
			return err
		}
		title := tmuxOpt("@darshan-title", " darshan ")
		c = exec.Command("sh", "-c", fmt.Sprintf("%s -E -T '%s' \"$0\"", strings.TrimSpace(string(exp)), title), run)
	default:
		c = exec.Command("tmux", "display-popup", "-c", client, "-E", "-w", tmuxOpt("@darshan-width", "90%"), "-h", tmuxOpt("@darshan-height", "88%"),
			"-T", tmuxOpt("@darshan-title", " darshan "), run)
	}
	// display-popup waits for the popup: it runs on its own
	c.Stdin, c.Stdout, c.Stderr = nil, nil, nil
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// Show puts a file in front of the user, from anywhere: in the running
// screen, a popup in tmux, or a terminal window from the desktop.
func Show(path string, page int, agent Agent, why, client string) error {
	if _, err := Send(Command{Cmd: "open", Path: path, Page: page, Agent: agent, Why: why}); err == nil {
		return nil
	}
	s := Load()
	s.File, s.Page = path, page
	if page <= 0 {
		s.Page = s.Pages[path]
	}
	if agent != (Agent{}) {
		s.Agent = agent
	} else {
		s.Agent = Agent{}
	}
	s.Remember(path)
	if err := Save(s); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	inTmux := os.Getenv("TMUX") != "" || agent.Pane != ""
	if inTmux {
		if c := Client(client, agent.Pane); c != "" {
			if err := OpenPopup(exe, c, s.Zen); err != nil {
				return err
			}
			return waitRunning()
		}
	}
	// no tmux here (a file manager, a browser): a window of its own
	if _, err := exec.LookPath("kitty"); err != nil {
		return fmt.Errorf("no tmux client here and no kitty to open a window: run darshan in a terminal")
	}
	w := exec.Command("kitty", "--class", "darshan", "-T", "darshan", exe)
	w.Stdin, w.Stdout, w.Stderr = nil, nil, nil
	if err := w.Start(); err != nil {
		return err
	}
	go func() { _ = w.Wait() }()
	return nil
}

func waitRunning() error {
	for range 30 {
		if _, err := Send(Command{Cmd: "status"}); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
