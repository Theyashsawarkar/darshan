package mcpserver

// One MCP server for every agent: `darshan serve` listens on a unix socket
// and runs one MCP session per connection, as the agent the connection's
// hello names; `darshan mcp` (internal/mcppipe) is the pipe to it.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Theyashsawarkar/darshan/internal/mcppipe"
	"github.com/Theyashsawarkar/darshan/internal/state"
)

// Idle is how long the daemon waits with no agent connected before it
// exits; DARSHAN_SERVE_IDLE (a Go duration) changes it, for tests.
var Idle = idleFromEnv()

func idleFromEnv() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("DARSHAN_SERVE_IDLE")); err == nil && d > 0 {
		return d
	}
	return 60 * time.Second
}

// Socket is where the daemon listens.
func Socket() string { return mcppipe.Socket() }

// errRunning means another daemon has the lock and answers.
var errRunning = errors.New("darshan serve is already running")

// lock takes the daemon's lock, waiting a little for one that is stepping
// down; it fails at once when another daemon answers on the socket.
func lock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "mcp.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for end := time.Now().Add(2 * time.Second); ; {
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return f, nil
		}
		if c, err := net.DialTimeout("unix", Socket(), 200*time.Millisecond); err == nil {
			c.Close()
			f.Close()
			return nil, errRunning
		}
		if time.Now().After(end) {
			f.Close()
			return nil, errRunning
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// CleanEnv takes out of this process's environment what the agent that
// started the daemon leaves in it and no other agent should get: an
// LD_PRELOAD or LD_LIBRARY_PATH (old shells carry a gtk4-layer-shell one
// that crashes programs), and the agent's own CLAUDE*, ANTHROPIC* and MCP_*
// variables. `darshan serve` calls it first.
func CleanEnv() {
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "LD_PRELOAD" || k == "LD_LIBRARY_PATH" || strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC") || strings.HasPrefix(k, "MCP_") {
			_ = os.Unsetenv(k)
		}
	}
}

// Serve is `darshan serve`: the daemon, for pipes from build (mcppipe.Build).
// It returns when it has had no connection for Idle, or once it has
// stepped down and its sessions ended. Another daemon running is no error.
func Serve(ctx context.Context, version, build string) error {
	dir := mcppipe.SocketDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)
	lk, err := lock(dir)
	if errors.Is(err, errRunning) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lk.Close()
	sock := Socket()
	_ = os.Remove(sock) // left by a daemon that died: we hold the lock
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", sock)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	_ = os.Chmod(sock, 0o600)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d := &daemon{version: version, build: build, idleFor: Idle, l: l, lock: lk, sock: sock}
	d.start()
	go func() {
		<-ctx.Done()
		d.stepDown()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			break
		}
		d.add(1)
		go func() {
			defer d.add(-1)
			d.session(ctx, conn)
		}()
	}
	// stopped listening: let the sessions still open finish
	d.mu.Lock()
	for d.n > 0 {
		d.cond.Wait()
	}
	d.mu.Unlock()
	return nil
}

type daemon struct {
	version string
	build   string
	idleFor time.Duration
	l       net.Listener
	lock    *os.File
	sock    string

	mu      sync.Mutex
	cond    *sync.Cond
	n       int // open sessions
	timer   *time.Timer
	stepped bool
}

func (d *daemon) add(delta int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n += delta
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.n == 0 {
		d.cond.Broadcast()
		if !d.stepped {
			d.timer = time.AfterFunc(d.idleFor, d.stepDown)
		}
	}
}

func (d *daemon) start() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cond = sync.NewCond(&d.mu)
	d.timer = time.AfterFunc(d.idleFor, d.stepDown)
}

// stepDown stops taking agents and frees the socket and lock for a new
// daemon; the sessions already open go on until their agents leave.
func (d *daemon) stepDown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stepped {
		return
	}
	d.stepped = true
	_ = d.l.Close()
	_ = os.Remove(d.sock)
	_ = syscall.Flock(int(d.lock.Fd()), syscall.LOCK_UN)
}

type connReader struct {
	*bufio.Reader
	io.Closer
}

// session runs one agent's MCP session on conn, as that agent's Origin.
func (d *daemon) session(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return
	}
	var h mcppipe.Hello
	reply := func(a mcppipe.Answer) bool { return json.NewEncoder(conn).Encode(a) == nil }
	switch err := json.Unmarshal(line, &h); {
	case err != nil || h.Proto != mcppipe.Proto:
		reply(mcppipe.Answer{Error: "not a darshan mcp hello"})
		return
	case h.Build != d.build:
		// a new darshan is installed: make room for its daemon
		reply(mcppipe.Answer{Error: fmt.Sprintf("darshan serve is %q, darshan mcp is %q", d.build, h.Build), Retry: true})
		d.stepDown()
		return
	case h.State != state.Dir():
		reply(mcppipe.Answer{Error: "darshan serve keeps its state in " + state.Dir() + ", not " + h.State})
		return
	}
	if !reply(mcppipe.Answer{OK: true}) {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	s := &server{origin: h.Origin, env: environ(h.Origin)}
	ss, err := newServer(d.version, s).Connect(ctx, &mcp.IOTransport{Reader: connReader{br, conn}, Writer: conn}, nil)
	if err != nil {
		return
	}
	_ = ss.Wait()
}
