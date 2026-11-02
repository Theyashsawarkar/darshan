// Package state is what darshan remembers (the open file, its page, zen,
// recent files, the talk with an agent) and how its processes talk: a unix
// socket the running screen listens on, and a queue of questions for agents.
package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Agent is who opened the file through MCP: where questions go.
type Agent struct {
	Name string `json:"name,omitempty"`
	Pane string `json:"pane,omitempty"` // the tmux pane it runs in
}

// Message is one line of the talk about a file.
type Message struct {
	File string `json:"file"`
	Who  string `json:"who"` // you, or the agent's name
	Text string `json:"text"`
	At   int64  `json:"at"`
}

// State survives closing the popup: the last file stays open until x.
type State struct {
	File   string         `json:"file,omitempty"`
	Page   int            `json:"page,omitempty"` // from 1; a line for Markdown
	Zen    bool           `json:"zen,omitempty"`
	Invert bool           `json:"invert,omitempty"`
	Recent []string       `json:"recent,omitempty"`
	Pages  map[string]int `json:"pages,omitempty"` // the last place in each file
	Agent  Agent          `json:"agent,omitempty"`
	Chat   []Message      `json:"chat,omitempty"`
}

// Remember makes path the newest recent file.
func (s *State) Remember(path string) {
	out := []string{path}
	for _, p := range s.Recent {
		if p != path && len(out) < 50 {
			out = append(out, p)
		}
	}
	s.Recent = out
}

func home(env, def string) string {
	if h := os.Getenv("DARSHAN_HOME"); h != "" {
		return h
	}
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, "darshan")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, def, "darshan")
}

// Dir holds state.json and the question queue.
func Dir() string { return home("XDG_STATE_HOME", ".local/state") }

// CacheDir holds rendered pages and the file index.
func CacheDir() string { return home("XDG_CACHE_HOME", ".cache") }

// ConfigDir holds config.toml.
func ConfigDir() string { return home("XDG_CONFIG_HOME", ".config") }

// Load reads the state; a missing or broken file is an empty state.
func Load() State {
	var s State
	b, err := os.ReadFile(filepath.Join(Dir(), "state.json"))
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Pages == nil {
		s.Pages = map[string]int{}
	}
	return s
}

// Save writes the state atomically.
func Save(s State) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	p := filepath.Join(Dir(), "state.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Socket is where the running screen listens.
func Socket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if os.Getenv("DARSHAN_HOME") != "" || dir == "" {
		dir = Dir()
	}
	return filepath.Join(dir, "darshan.sock")
}

// Command is sent to the running screen.
type Command struct {
	Cmd   string `json:"cmd"` // open, reply, close, hide, status
	Path  string `json:"path,omitempty"`
	Page  int    `json:"page,omitempty"`
	Text  string `json:"text,omitempty"`
	Agent Agent  `json:"agent,omitempty"`
	Why   string `json:"why,omitempty"`
}

// Status is what the screen shows now.
type Status struct {
	Running bool   `json:"running"`
	File    string `json:"file,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Page    int    `json:"page,omitempty"`
	Pages   int    `json:"pages,omitempty"`
	Visible string `json:"visible,omitempty"`
}

// Response answers a Command.
type Response struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Status Status `json:"status"`
}

// Send gives the running screen a command; an error means none is running.
func Send(c Command) (Response, error) {
	var r Response
	conn, err := net.DialTimeout("unix", Socket(), time.Second)
	if err != nil {
		return r, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(conn).Encode(c); err != nil {
		return r, err
	}
	err = json.NewDecoder(bufio.NewReader(conn)).Decode(&r)
	if err == nil && r.Error != "" {
		return r, errors.New(r.Error)
	}
	return r, err
}

// Listen serves commands until the listener closes; it replaces a socket
// left behind by a crash, but refuses when another screen answers.
func Listen(handle func(Command) Response) (net.Listener, error) {
	p := Socket()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", p, 300*time.Millisecond); err == nil {
		c.Close()
		return nil, errors.New("darshan is already running")
	}
	_ = os.Remove(p)
	l, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(p, 0o600)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				var c Command
				if json.NewDecoder(conn).Decode(&c) == nil {
					_ = json.NewEncoder(conn).Encode(handle(c))
				}
			}()
		}
	}()
	return l, nil
}
