package mcpserver

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Theyashsawarkar/darshan/internal/mcppipe"
	"github.com/Theyashsawarkar/darshan/internal/state"
)

// home gives the test its own darshan home and a fake screen that records
// what it is asked to open (so nothing opens a popup or a window).
func home(t *testing.T) (opened func() []state.Command) {
	t.Helper()
	t.Setenv("DARSHAN_HOME", t.TempDir())
	t.Setenv("DARSHAN_ROOTS", t.TempDir())
	var mu sync.Mutex
	var got []state.Command
	l, err := state.Listen(func(c state.Command) state.Response {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, c)
		return state.Response{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return func() []state.Command {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func serve(t *testing.T, build string) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "test", build) }()
	for range 100 {
		if c, err := net.Dial("unix", Socket()); err == nil {
			c.Close()
			return done
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("serve didn't listen")
	return nil
}

// session joins the daemon as an agent named name, from origin.
func session(t *testing.T, name string, o Origin) *mcp.ClientSession {
	t.Helper()
	conn, err := mcppipe.Connect(mcppipe.Hello{Proto: mcppipe.Proto, Build: "test", State: state.Dir(), Origin: o}, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if r.IsError {
		t.Fatalf("%s: %s", tool, b.String())
	}
	return b.String()
}

// Two agents at once through one daemon: each keeps its own name, pane and
// directory, as when each ran its own darshan mcp.
func TestTwoSessionsKeepTheirIdentity(t *testing.T) {
	opened := home(t)
	serve(t, "test")
	dirA, dirB := t.TempDir(), t.TempDir()
	for _, d := range []string{dirA, dirB} {
		if err := os.WriteFile(filepath.Join(d, "notes.md"), []byte("# hi\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := session(t, "claude-code", Origin{Cwd: dirA, Env: map[string]string{"TMUX_PANE": "%1", "TMUX": "/tmp/a,1,0"}})
	b := session(t, "codex-cli", Origin{Cwd: dirB, Env: map[string]string{"TMUX_PANE": "%2"}})

	var wg sync.WaitGroup
	for _, cs := range []*mcp.ClientSession{a, b, a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			call(t, cs, "view_open", map[string]any{"path": "notes.md"})
		}()
	}
	wg.Wait()
	want := map[string]state.Agent{
		filepath.Join(dirA, "notes.md"): {Name: "Claude Code", Pane: "%1"},
		filepath.Join(dirB, "notes.md"): {Name: "Codex", Pane: "%2"},
	}
	got := opened()
	if len(got) != 4 {
		t.Fatalf("opened %d times, want 4: %+v", len(got), got)
	}
	for _, c := range got {
		if w, ok := want[c.Path]; !ok || c.Agent != w {
			t.Errorf("opened %s as %+v, want %+v", c.Path, c.Agent, w)
		}
	}
	call(t, b, "view_reply", map[string]any{"text": "an answer"})
	if r := opened()[4]; r.Cmd != "reply" || r.Agent != (state.Agent{Name: "Codex", Pane: "%2"}) {
		t.Errorf("reply went as %+v", r)
	}
}

// Commands run for an agent see its tmux server and pane, not the daemon's.
func TestOriginEnviron(t *testing.T) {
	t.Setenv("TMUX", "/tmp/daemons,1,0")
	t.Setenv("TMUX_PANE", "%9")
	env := environ(Origin{Env: map[string]string{"WAYLAND_DISPLAY": "wayland-1"}})
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			t.Errorf("daemon's %s leaked to an agent outside tmux", kv)
		}
	}
	if !slices.Contains(env, "WAYLAND_DISPLAY=wayland-1") {
		t.Error("agent's desktop is missing")
	}
	env = environ(Origin{Env: map[string]string{"TMUX": "/tmp/mine,2,0", "TMUX_PANE": "%3"}})
	if !slices.Contains(env, "TMUX=/tmp/mine,2,0") || !slices.Contains(env, "TMUX_PANE=%3") || slices.Contains(env, "TMUX=/tmp/daemons,1,0") {
		t.Errorf("agent's tmux not used: %v", env)
	}
}

// With nobody connected for Idle, the daemon exits and frees its socket;
// a session holds it open.
func TestIdleExit(t *testing.T) {
	home(t)
	old := Idle
	Idle = 300 * time.Millisecond
	t.Cleanup(func() { Idle = old })
	done := serve(t, "test")
	cs := session(t, "claude-code", Origin{})
	time.Sleep(2 * Idle)
	select {
	case <-done:
		t.Fatal("exited with a session open")
	default:
	}
	call(t, cs, "view_status", nil)
	cs.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't exit when idle")
	}
	if _, err := os.Stat(Socket()); !os.IsNotExist(err) {
		t.Error("socket left behind")
	}
}

// Only one daemon: a second one sees the first and leaves.
func TestOneDaemon(t *testing.T) {
	home(t)
	serve(t, "test")
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), "test", "test") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a second daemon kept running")
	}
}

// An old daemon steps down for a newly installed darshan mcp, keeping its sessions.
func TestOldDaemonStepsDown(t *testing.T) {
	home(t)
	done := serve(t, "old")
	_, err := mcppipe.Connect(mcppipe.Hello{Proto: mcppipe.Proto, Build: "new", State: state.Dir()}, "/nonexistent")
	if err == nil {
		t.Fatal("an old daemon took a new pipe")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old daemon didn't step down")
	}
	serve(t, "new") // the lock is free again
}

// The real binary: darshan mcp starts the daemon and pipes to it; when
// it can't, it serves in process.
func TestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds darshan")
	}
	bin := filepath.Join(t.TempDir(), "darshan")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/darshan").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	run := func(t *testing.T, home string) (*mcp.ClientSession, *bytes.Buffer) {
		cmd := exec.Command(bin, "mcp")
		cmd.Env = append(os.Environ(), "DARSHAN_HOME="+home, "DARSHAN_ROOTS="+t.TempDir(), "DARSHAN_SERVE_IDLE=500ms")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		call(t, cs, "view_status", nil)
		return cs, &stderr
	}

	t.Run("daemon", func(t *testing.T) {
		h := t.TempDir()
		t.Setenv("DARSHAN_HOME", h)
		a, stderr := run(t, h)
		b, _ := run(t, h)
		if _, err := os.Stat(Socket()); err != nil {
			t.Fatal("no daemon:", err)
		}
		a.Close() // waits for the process: stderr is complete
		b.Close()
		if stderr.Len() > 0 {
			t.Errorf("stderr: %s", stderr)
		}
		for range 100 { // it leaves Idle after the last agent
			if _, err := os.Stat(Socket()); os.IsNotExist(err) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Error("daemon still there after idle")
	})

	// mcppipe's init runs the pipe before the screen's packages initialise
	t.Run("light", func(t *testing.T) {
		h := t.TempDir()
		cmd := exec.Command(bin, "mcp")
		cmd.Env = append(os.Environ(), "DARSHAN_HOME="+h, "DARSHAN_ROOTS="+t.TempDir(), "DARSHAN_SERVE_IDLE=200ms", "GODEBUG=inittrace=1")
		cmd.Stdin = strings.NewReader("") // the agent leaves at once
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		trace := string(out) // mcppipe's own line never shows: it exits in init
		if !strings.Contains(trace, "init os @") {
			t.Fatalf("no init trace: %s", trace)
		}
		for _, heavy := range []string{"chroma", "glamour", "bluemonday", "goldmark", "go-runewidth", "go-sdk/mcp ", "bubbletea", "init net @"} {
			if strings.Contains(trace, heavy) {
				t.Errorf("the pipe initialised %s", heavy)
			}
		}
	})

	t.Run("fallback", func(t *testing.T) {
		h := filepath.Join(t.TempDir(), "ro")
		if err := os.Mkdir(h, 0o500); err != nil { // no room for a socket or a log
			t.Fatal(err)
		}
		cs, stderr := run(t, h)
		cs.Close()
		if !strings.Contains(stderr.String(), "serving in this process") {
			t.Errorf("stderr: %q", stderr)
		}
	})
}

// An agent that leaves while it waits for a question stops waiting: else
// the daemon would keep saying someone listens, and questions would queue
// instead of reaching the other agents' panes.
func TestLeavingStopsWaiting(t *testing.T) {
	home(t)
	serve(t, "test")
	conn, err := mcppipe.Connect(mcppipe.Hello{Proto: mcppipe.Proto, Build: "test", State: state.Dir()}, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "1"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "view_next_question", Arguments: map[string]any{"timeout_seconds": 60}})
	}()
	waiter := filepath.Join(state.Dir(), "waiter")
	for range 100 {
		if _, err := os.Stat(waiter); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	conn.Close() // the pipe dies with its agent
	time.Sleep(time.Second)
	fi, err := os.Stat(waiter)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	if fi2, _ := os.Stat(waiter); !fi2.ModTime().Equal(fi.ModTime()) {
		t.Error("still waiting after the agent left")
	}
}
