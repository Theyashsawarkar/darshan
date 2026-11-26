package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Theyashsawarkar/darshan/internal/state"
)

// home gives the test its own darshan home (and so its own daemon socket,
// $DARSHAN_HOME/run/mcp.sock) and a fake screen that records what it is
// asked to do, taking slow over each "open" (so nothing opens a popup or a
// window).
func home(t *testing.T, slow time.Duration) (opened func() []state.Command) {
	t.Helper()
	t.Setenv("DARSHAN_HOME", t.TempDir())
	t.Setenv("DARSHAN_ROOTS", t.TempDir())
	t.Setenv("PRANALI_SOCKET", "")
	var mu sync.Mutex
	var got []state.Command
	l, err := state.Listen(func(c state.Command) state.Response {
		if c.Cmd == "open" {
			time.Sleep(slow)
		}
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

// serve runs the daemon in this process; done closes when it returns,
// stop is its SIGTERM.
func serve(t *testing.T) (done <-chan struct{}, stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{})
	var err error
	go func() { err = Serve(ctx, "test"); close(ch) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-ch:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(30 * time.Second):
			t.Error("serve didn't stop")
		}
	})
	waitSocket(t)
	return ch, cancel
}

func waitSocket(t *testing.T) {
	t.Helper()
	for range 200 {
		if c, err := net.Dial("unix", Socket()); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("serve didn't listen")
}

// pranaliHello is the hello pranali sends for an agent at o.
func pranaliHello(o Origin) string {
	b, _ := json.Marshal(map[string]any{"pranali": 1, "tool": "darshan", "agent": "claude", "pid": o.Pid, "cwd": o.Cwd, "env": o.Env, "future": "ignored"})
	return string(b)
}

// legacyHello is darshan 0.2's pipe's hello, from another build.
func legacyHello(o Origin) string {
	b, _ := json.Marshal(map[string]any{"proto": legacyProto, "build": "/old/darshan 123 456", "state": state.Dir(), "origin": o})
	return string(b)
}

// greetWith connects as a minimal pranali client: hello, then the answer
// read byte by byte, so nothing after it is lost.
func greetWith(t *testing.T, line string) (net.Conn, answer) {
	t.Helper()
	c, err := net.Dial("unix", Socket())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	var got []byte
	b := make([]byte, 1)
	for {
		if _, err := c.Read(b); err != nil {
			t.Fatalf("no answer to the hello: %v", err)
		}
		if b[0] == '\n' {
			break
		}
		got = append(got, b[0])
	}
	var a answer
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("answer %q: %v", got, err)
	}
	return c, a
}

// session joins the daemon as an agent named name, with hello.
func session(t *testing.T, name, hello string) *mcp.ClientSession {
	t.Helper()
	conn, a := greetWith(t, hello)
	if !a.OK || a.Pranali != 1 {
		t.Fatalf("hello refused: %+v", a)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: name, Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callErr(cs *mcp.ClientSession, tool string, args map[string]any) (string, error) {
	r, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if r.IsError {
		return b.String(), errors.New(b.String())
	}
	return b.String(), nil
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) string {
	t.Helper()
	s, err := callErr(cs, tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return s
}

func notes(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "notes.md"), []byte("# hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

// Two agents at once through one daemon, both through pranali's hello:
// each keeps its own name, pane and directory, as when each ran its own
// darshan mcp.
func TestTwoSessionsKeepTheirIdentity(t *testing.T) {
	opened := home(t, 0)
	serve(t)
	dirA, dirB := notes(t), notes(t)
	a := session(t, "claude-code", pranaliHello(Origin{Pid: 1, Cwd: dirA, Env: map[string]string{"TMUX_PANE": "%1", "TMUX": "/run/a,1,0"}}))
	b := session(t, "codex-cli", pranaliHello(Origin{Pid: 2, Cwd: dirB, Env: map[string]string{"TMUX_PANE": "%2"}}))

	var wg sync.WaitGroup
	for _, cs := range []*mcp.ClientSession{a, b, a, b} {
		wg.Go(func() { call(t, cs, "view_open", map[string]any{"path": "notes.md"}) })
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

// The hello of darshan 0.2's pipe still works, from any build: a pipe from
// before an update reaches the new daemon, which never steps down for it,
// and the next agent (pranali) is served by the same daemon.
func TestLegacyHelloNoStepDown(t *testing.T) {
	opened := home(t, 0)
	done, _ := serve(t)
	dir := notes(t)
	old := session(t, "claude-code", legacyHello(Origin{Pid: 3, Cwd: dir, Env: map[string]string{"TMUX_PANE": "%4"}}))
	call(t, old, "view_open", map[string]any{"path": "notes.md"})
	if got := opened(); len(got) != 1 || got[0].Path != filepath.Join(dir, "notes.md") || got[0].Agent.Pane != "%4" {
		t.Fatalf("legacy session opened %+v", got)
	}
	for range 3 { // other builds, again and again: no step-down
		session(t, "claude-code", legacyHello(Origin{}))
	}
	select {
	case <-done:
		t.Fatal("daemon left")
	case <-time.After(200 * time.Millisecond):
	}
	call(t, old, "view_status", nil)
	call(t, session(t, "claude-code", pranaliHello(Origin{})), "view_status", nil)
}

// Hellos it can't serve are answered ok:false, in pranali's format.
func TestHelloRefused(t *testing.T) {
	home(t, 0)
	serve(t)
	for _, line := range []string{
		`{"pranali":1,"tool":"narada"}`,
		`{"pranali":2,"tool":"darshan"}`,
		`{"proto":"darshan-mcp/1","state":"/elsewhere"}`,
		`hello`,
	} {
		c, a := greetWith(t, line)
		c.Close()
		if a.OK || a.Pranali != 1 || a.Error == "" {
			t.Errorf("%s: answered %+v", line, a)
		}
	}
}

// Commands run for an agent see its tmux server and pane, not the daemon's.
func TestOriginEnviron(t *testing.T) {
	t.Setenv("TMUX", "/run/daemons,1,0")
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
	env = environ(Origin{Env: map[string]string{"TMUX": "/run/mine,2,0", "TMUX_PANE": "%3"}})
	if !slices.Contains(env, "TMUX=/run/mine,2,0") || !slices.Contains(env, "TMUX_PANE=%3") || slices.Contains(env, "TMUX=/run/daemons,1,0") {
		t.Errorf("agent's tmux not used: %v", env)
	}
}

// Only the six variables of pranali's hello are taken from it.
func TestHelloEnvFiltered(t *testing.T) {
	o, err := parseHello([]byte(`{"pranali":1,"tool":"darshan","agent":"codex","pid":7,"cwd":"/w","env":{"TMUX_PANE":"%1","LD_PRELOAD":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Pid != 7 || o.Cwd != "/w" || o.Agent != "codex" || len(o.Env) != 1 || o.Env["TMUX_PANE"] != "%1" {
		t.Errorf("origin %+v", o)
	}
}

// Each agent's commands come from its own PATH; with none sent, the daemon's.
func TestOriginPath(t *testing.T) {
	t.Setenv("PATH", "/daemon/bin")
	if env := environ(Origin{Env: map[string]string{"PATH": "/agent/bin"}}); !slices.Contains(env, "PATH=/agent/bin") || slices.Contains(env, "PATH=/daemon/bin") {
		t.Errorf("agent's PATH not used: %v", env)
	}
	if env := environ(Origin{}); !slices.Contains(env, "PATH=/daemon/bin") {
		t.Errorf("no PATH at all: %v", env)
	}
}

// The daemon doesn't hand its starter's preload or Claude's variables on.
func TestCleanEnv(t *testing.T) {
	for _, k := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "ANTHROPIC_API_KEY", "MCP_TIMEOUT"} {
		t.Setenv(k, "x")
	}
	t.Setenv("DARSHAN_KEEP", "y")
	CleanEnv()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "LD_PRELOAD" || k == "LD_LIBRARY_PATH" || strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC") || strings.HasPrefix(k, "MCP_") {
			t.Errorf("%s left in", k)
		}
	}
	if os.Getenv("DARSHAN_KEEP") != "y" {
		t.Error("took too much")
	}
}

// The socket path follows pranali's rules, never /tmp.
func TestSocketPath(t *testing.T) {
	t.Setenv("DARSHAN_HOME", "")
	t.Setenv("PRANALI_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/42")
	if s := Socket(); s != "/run/user/42/darshan/mcp.sock" {
		t.Error(s)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", "/home/u/.state")
	if s := Socket(); s != "/home/u/.state/darshan/run/mcp.sock" {
		t.Error(s)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if s := Socket(); s != "/home/u/.local/state/darshan/run/mcp.sock" {
		t.Error(s)
	}
	t.Setenv("DARSHAN_HOME", "/home/u/dh")
	if s := Socket(); s != "/home/u/dh/run/mcp.sock" {
		t.Error(s)
	}
	t.Setenv("PRANALI_SOCKET", "/run/user/42/x.sock")
	if s := Socket(); s != "/run/user/42/x.sock" {
		t.Error(s)
	}
	if _, _, err := bind("/run/" + strings.Repeat("x", 120) + "/mcp.sock"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("long path: %v", err)
	}
}

// systemd's variables: only for this pid, the named socket among several.
func TestListenFD(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		env  map[string]string
		want int
		err  bool
	}{
		{map[string]string{}, -1, false},
		{map[string]string{"LISTEN_PID": "99", "LISTEN_FDS": "1"}, -1, false},
		{map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}, 3, false},
		{map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "other:darshan-mcp.socket"}, 4, false},
		{map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "a:b"}, -1, true},
	} {
		got, err := listenFD(env(c.env), 42)
		if got != c.want || (err != nil) != c.err {
			t.Errorf("%v: %d, %v", c.env, got, err)
		}
	}
}

// With nobody connected for Idle, the daemon exits and removes the socket
// it bound; a session holds it open.
func TestIdleExit(t *testing.T) {
	home(t, 0)
	old := Idle
	Idle = 300 * time.Millisecond
	t.Cleanup(func() { Idle = old })
	done, _ := serve(t)
	cs := session(t, "claude-code", pranaliHello(Origin{}))
	time.Sleep(2 * Idle)
	select {
	case <-done:
		t.Fatal("exited with a session open")
	default:
	}
	call(t, cs, "view_status", nil)
	cs.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("didn't exit when idle")
	}
	if _, err := os.Stat(Socket()); !os.IsNotExist(err) {
		t.Error("socket left behind")
	}
}

// Only one daemon: a second one sees the first and leaves, without
// touching its socket.
func TestOneDaemon(t *testing.T) {
	home(t, 0)
	serve(t)
	if err := Serve(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	call(t, session(t, "claude-code", pranaliHello(Origin{})), "view_status", nil)
}

// A socket left by a daemon that died is replaced; something else in the
// way is not removed.
func TestStaleSocket(t *testing.T) {
	home(t, 0)
	if err := os.MkdirAll(filepath.Dir(Socket()), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", Socket())
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close() // dead: the file stays, nobody answers
	serve(t)
	call(t, session(t, "claude-code", pranaliHello(Origin{})), "view_status", nil)

	home(t, 0)
	_ = os.MkdirAll(filepath.Dir(Socket()), 0o700)
	if err := os.WriteFile(Socket(), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Serve(context.Background(), "test"); err == nil {
		t.Fatal("served over a file in the way")
	}
	if b, _ := os.ReadFile(Socket()); string(b) != "mine" {
		t.Error("removed a file that wasn't a socket")
	}
}

// SIGTERM: no new agents, a waiting view_next_question ends at once, a
// call in flight finishes and is answered, new calls are turned away,
// then the daemon exits.
func TestDrain(t *testing.T) {
	opened := home(t, 1500*time.Millisecond)
	done, stop := serve(t)
	dir := notes(t)
	cs := session(t, "claude-code", pranaliHello(Origin{Cwd: dir}))
	other := session(t, "codex", pranaliHello(Origin{}))
	waiting := make(chan error, 1)
	go func() {
		_, err := callErr(cs, "view_next_question", map[string]any{"timeout_seconds": 60})
		waiting <- err
	}()
	slow := make(chan error, 1)
	go func() {
		_, err := callErr(other, "view_open", map[string]any{"path": filepath.Join(dir, "notes.md")})
		slow <- err
	}()
	waitFile(t, filepath.Join(state.Dir(), "waiter"))
	time.Sleep(200 * time.Millisecond) // view_open is at the screen
	t0 := time.Now()
	stop()
	select {
	case err := <-waiting:
		if err == nil || !strings.Contains(err.Error(), "restarting") {
			t.Errorf("view_next_question: %v", err)
		}
		if d := time.Since(t0); d > time.Second {
			t.Errorf("view_next_question held shutdown for %v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("view_next_question still waits")
	}
	if _, err := net.Dial("unix", Socket()); err == nil {
		t.Error("still taking agents")
	}
	if _, err := callErr(cs, "view_status", nil); err == nil || !strings.Contains(err.Error(), "restarting") {
		t.Errorf("a new call while draining: %v", err)
	}
	select {
	case <-done:
		t.Fatal("exited with a call in flight")
	default:
	}
	if err := <-slow; err != nil {
		t.Errorf("call in flight: %v", err)
	}
	if len(opened()) != 1 {
		t.Error("view_open didn't happen")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("didn't exit after the drain")
	}
}

// A drain doesn't wait forever: past Drain the connections are cut.
func TestDrainLimit(t *testing.T) {
	home(t, 3*time.Second)
	old := Drain
	Drain = 300 * time.Millisecond
	t.Cleanup(func() { Drain = old })
	done, stop := serve(t)
	dir := notes(t)
	cs := session(t, "claude-code", pranaliHello(Origin{Cwd: dir}))
	go func() { _, _ = callErr(cs, "view_open", map[string]any{"path": "notes.md"}) }()
	time.Sleep(200 * time.Millisecond)
	t0 := time.Now()
	stop()
	select {
	case <-done:
		if d := time.Since(t0); d > 2*time.Second {
			t.Errorf("took %v", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("drain didn't stop at its limit")
	}
}

// Half-open: the agent stops writing (pranali at its stdin's end) and
// still gets the answer to the call it made.
func TestHalfOpen(t *testing.T) {
	home(t, 500*time.Millisecond)
	serve(t)
	dir := notes(t)
	c, a := greetWith(t, pranaliHello(Origin{Cwd: dir}))
	if !a.OK {
		t.Fatal(a)
	}
	defer c.Close()
	for _, m := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"x2","method":"tools/call","params":{"name":"view_open","arguments":{"path":"notes.md"}}}`,
	} {
		if _, err := c.Write([]byte(m + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var ids []string
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var r struct {
			ID     json.RawMessage `json:"id"`
			Result *struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		_ = json.Unmarshal(sc.Bytes(), &r)
		ids = append(ids, string(r.ID))
		if string(r.ID) == `"x2"` && (r.Result == nil || r.Result.IsError) {
			t.Errorf("view_open answered %s", sc.Bytes())
		}
	}
	if !slices.Equal(ids, []string{"1", `"x2"`}) {
		t.Errorf("answers %v, want 1 and \"x2\"", ids)
	}
}

// An agent that leaves while it waits for a question stops waiting: else
// the daemon would keep saying someone listens, and questions would queue
// instead of reaching the other agents' panes.
func TestLeavingStopsWaiting(t *testing.T) {
	home(t, 0)
	serve(t)
	conn, a := greetWith(t, pranaliHello(Origin{}))
	if !a.OK {
		t.Fatal(a)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "claude-code", Version: "1"}, nil).Connect(context.Background(), &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = callErr(cs, "view_next_question", map[string]any{"timeout_seconds": 60}) }()
	waiter := filepath.Join(state.Dir(), "waiter")
	waitFile(t, waiter)
	conn.Close() // the agent goes
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

func waitFile(t *testing.T, p string) {
	t.Helper()
	for range 200 {
		if _, err := os.Stat(p); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", p)
}

// The real binary.
func TestBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds darshan")
	}
	bin := filepath.Join(t.TempDir(), "darshan")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/darshan").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	// systemd's part, done by hand: the socket bound outside the daemon,
	// passed as fd 3 with LISTEN_PID/LISTEN_FDS set for the daemon's pid
	activate := func(t *testing.T, l *os.File, idle string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", `LISTEN_PID=$$ LISTEN_FDS=1 LISTEN_FDNAMES=darshan-mcp.socket exec "$0" serve`, bin)
		cmd.ExtraFiles = []*os.File{l}
		cmd.Env = append(os.Environ(), "DARSHAN_SERVE_IDLE="+idle)
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	exited := func(t *testing.T, cmd *exec.Cmd, within time.Duration) {
		t.Helper()
		ch := make(chan error, 1)
		go func() { ch <- cmd.Wait() }()
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("serve: %v", err)
			}
		case <-time.After(within):
			_ = cmd.Process.Kill()
			t.Fatal("serve didn't exit")
		}
	}
	socketFile := func(t *testing.T) *os.File {
		if err := os.MkdirAll(filepath.Dir(Socket()), 0o700); err != nil {
			t.Fatal(err)
		}
		l, err := net.Listen("unix", Socket())
		if err != nil {
			t.Fatal(err)
		}
		f, err := l.(*net.UnixListener).File()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close(); l.Close() })
		return f
	}

	t.Run("activation", func(t *testing.T) {
		home(t, 0)
		f := socketFile(t)
		cmd := activate(t, f, "500ms")
		cs := session(t, "claude-code", pranaliHello(Origin{}))
		call(t, cs, "view_status", nil)
		cs.Close()
		exited(t, cmd, 5*time.Second) // idle
		if _, err := os.Stat(Socket()); err != nil {
			t.Fatal("the daemon removed systemd's socket:", err)
		}
		// an agent comes while nothing runs: it waits in the backlog ...
		got := make(chan *mcp.ClientSession, 1)
		go func() { got <- session(t, "claude-code", pranaliHello(Origin{})) }()
		time.Sleep(300 * time.Millisecond)
		// ... until systemd starts the daemon again
		cmd = activate(t, f, "500ms")
		select {
		case cs := <-got:
			call(t, cs, "view_status", nil)
			cs.Close()
		case <-time.After(5 * time.Second):
			t.Fatal("the waiting agent wasn't served")
		}
		exited(t, cmd, 5*time.Second)
	})

	t.Run("sigterm", func(t *testing.T) {
		home(t, 0)
		cmd := activate(t, socketFile(t), "1m")
		cs := session(t, "claude-code", pranaliHello(Origin{}))
		call(t, cs, "view_status", nil)
		_ = cmd.Process.Signal(syscall.SIGTERM)
		exited(t, cmd, 5*time.Second)
	})

	mcpRun := func(t *testing.T, path string) (*bytes.Buffer, string) {
		cmd := exec.Command(bin, "mcp")
		cmd.Env = append(os.Environ(), "PATH="+path)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("%v: %s", err, stderr.String())
		}
		out := call(t, cs, "view_status", nil)
		cs.Close()
		return &stderr, out
	}

	t.Run("no pranali", func(t *testing.T) {
		home(t, 0)
		stderr, _ := mcpRun(t, t.TempDir())
		if s := stderr.String(); strings.Count(s, "\n") != 1 || !strings.Contains(s, "no pranali on PATH: serving in this process") {
			t.Errorf("stderr: %q", s)
		}
	})

	// darshan mcp execs pranali, which runs the fallback here (no daemon)
	t.Run("pranali", func(t *testing.T) {
		home(t, 0)
		dir := t.TempDir()
		log := filepath.Join(dir, "argv")
		script := "#!/bin/sh\nfor a; do echo \"$a\"; done > " + log + "\necho \"PRANALI_SOCKET=$PRANALI_SOCKET\" >> " + log + "\nshift 2\nexec \"$@\"\n"
		if err := os.WriteFile(filepath.Join(dir, "pranali"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		stderr, _ := mcpRun(t, dir)
		b, _ := os.ReadFile(log)
		want := "darshan\n--fallback\n" + bin + "\nmcp\n--in-process\nPRANALI_SOCKET=" + Socket() + "\n"
		if string(b) != want {
			t.Errorf("pranali ran as\n%s\nwant\n%s", b, want)
		}
		if !strings.Contains(stderr.String(), "no MCP daemon answers") {
			t.Errorf("stderr: %q", stderr)
		}
	})
}
