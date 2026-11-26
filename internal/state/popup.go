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
func Client(given, pane string) string { return clientIn(given, pane, nil) }

// tmux runs tmux in env (nil: this process's own): an agent's TMUX names
// the tmux server it runs in.
func tmux(env []string, args ...string) *exec.Cmd {
	c := exec.Command("tmux", args...)
	c.Env = env
	return c
}

func clientIn(given, pane string, env []string) string {
	if given != "" {
		return given
	}
	if pane != "" {
		if out, err := tmux(env, "display", "-p", "-t", pane, "#{client_name}").Output(); err == nil {
			if c := strings.TrimSpace(string(out)); c != "" {
				return c
			}
		}
	}
	out, err := tmux(env, "list-clients", "-F", "#{client_activity} #{client_name}").Output()
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

func tmuxOpt(name, def string) string { return tmuxOptIn(nil, name, def) }

func tmuxOptIn(env []string, name, def string) string {
	out, err := tmux(env, "show-option", "-gqv", name).Output()
	if v := strings.TrimSpace(string(out)); err == nil && v != "" {
		return v
	}
	return def
}

// OpenPopup shows the screen in a tmux popup: full screen in zen mode, else
// at the plugin's size or through its launcher (@darshan-launcher).
func OpenPopup(exe, client string, zen bool) error { return openPopupIn(exe, client, zen, nil) }

func openPopupIn(exe, client string, zen bool, env []string) error {
	if client == "" {
		return fmt.Errorf("no tmux client to show darshan on")
	}
	run := fmt.Sprintf("'%s' --popup --client '%s'", exe, client)
	var c *exec.Cmd
	launcher := tmuxOptIn(env, "@darshan-launcher", "")
	switch {
	case zen:
		c = tmux(env, "display-popup", "-c", client, "-E", "-B", "-w", "100%", "-h", "100%", "-x", "C", "-y", "C", run)
	case launcher != "":
		exp, err := tmux(env, "display", "-p", "-c", client, launcher).Output()
		if err != nil {
			return err
		}
		title := tmuxOptIn(env, "@darshan-title", " darshan ")
		c = exec.Command("sh", "-c", fmt.Sprintf("%s -E -T '%s' \"$0\"", strings.TrimSpace(string(exp)), title), run)
	default:
		c = tmux(env, "display-popup", "-c", client, "-E", "-w", tmuxOptIn(env, "@darshan-width", "90%"), "-h", tmuxOptIn(env, "@darshan-height", "88%"),
			"-T", tmuxOptIn(env, "@darshan-title", " darshan "), run)
	}
	c.Env = env
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
	return ShowIn(path, page, agent, why, client, nil)
}

// ShowIn is Show from an agent's environment env (nil: this process's own),
// for the MCP daemon, which serves agents in other tmux servers and desktops.
func ShowIn(path string, page int, agent Agent, why, client string, env []string) error {
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
	inTmux := getenv(env, "TMUX") != "" || agent.Pane != ""
	if inTmux {
		if c := clientIn(client, agent.Pane, env); c != "" {
			if err := openPopupIn(exe, c, s.Zen, env); err != nil {
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
	w.Env = env
	w.Stdin, w.Stdout, w.Stderr = nil, nil, nil
	if err := w.Start(); err != nil {
		return err
	}
	go func() { _ = w.Wait() }()
	return nil
}

// getenv reads key from env, or from this process's environment when env is nil.
func getenv(env []string, key string) string {
	if env == nil {
		return os.Getenv(key)
	}
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
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

// PopupKey is the key that opens the popup (@darshan-key, default v;
// "" when it is "none"): inside the popup it closes it too.
func PopupKey() string {
	k := tmuxOpt("@darshan-key", "v")
	if k == "none" {
		return ""
	}
	return teaKey(k)
}

// teaKey spells a tmux key name ("C-v", "M-v", "F5", "Space") the way
// the screen sees it (Bubble Tea's "ctrl+v", "alt+v", "f5", " ").
func teaKey(k string) string {
	ctrl, alt := false, false
	for len(k) > 2 && k[1] == '-' {
		switch k[0] {
		case 'C', 'c':
			ctrl = true
		case 'M', 'm':
			alt = true
		case 'S', 's':
			k = k[:2] + strings.ToUpper(k[2:])
		}
		k = k[2:]
	}
	switch {
	case k == "Space":
		k = " "
	case len(k) > 1:
		k = strings.ToLower(k)
	case ctrl:
		k = strings.ToLower(k)
	}
	if ctrl {
		k = "ctrl+" + k
	}
	if alt {
		k = "alt+" + k
	}
	return k
}
