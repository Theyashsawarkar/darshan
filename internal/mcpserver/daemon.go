package mcpserver

// One MCP server for every agent: `darshan serve` listens on a unix socket
// (systemd's darshan-mcp.socket, or its own) and runs one MCP session per
// connection, as the agent the connection's hello names. Agents reach it
// through pranali (`darshan mcp` execs it).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Idle is how long the daemon waits with no agent connected before it
// exits (systemd starts it again on the next connection); DARSHAN_SERVE_IDLE
// (a Go duration) changes it, for tests.
var Idle = durationFromEnv("DARSHAN_SERVE_IDLE", 60*time.Second)

// Drain is how long the calls in flight get to finish on SIGTERM.
var Drain = 20 * time.Second

func durationFromEnv(k string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(k)); err == nil && d > 0 {
		return d
	}
	return def
}

// CleanEnv takes out of this process's environment what the agent or
// shell that started the daemon leaves in it and no other agent should
// get: an LD_PRELOAD or LD_LIBRARY_PATH (old shells carry a
// gtk4-layer-shell one that crashes programs), and the agent's own
// CLAUDE*, ANTHROPIC* and MCP_* variables. `darshan serve` calls it first.
func CleanEnv() {
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "LD_PRELOAD" || k == "LD_LIBRARY_PATH" || strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC") || strings.HasPrefix(k, "MCP_") {
			_ = os.Unsetenv(k)
		}
	}
}

// Serve is `darshan serve`. It returns after Idle with no agent, or when
// ctx ends (SIGTERM) and the calls in flight have finished (at most
// Drain). Another daemon already answering on the socket is no error.
func Serve(ctx context.Context, version string) error {
	l, stop, err := listen()
	if errors.Is(err, errRunning) {
		fmt.Fprintln(os.Stderr, "darshan serve: already running at", Socket())
		return nil
	}
	if err != nil {
		return err
	}
	d := &daemon{version: version, l: l, stopListening: stop, conns: map[*conn]bool{},
		changed: make(chan struct{}, 1), idle: make(chan struct{}), draining: make(chan struct{})}
	return d.run(ctx)
}

type daemon struct {
	version       string
	l             net.Listener
	stopListening func()

	mu       sync.Mutex
	n        int // connections open
	conns    map[*conn]bool
	pending  int // requests in flight, over every connection
	changed  chan struct{}
	timer    *time.Timer
	stopped  bool
	idle     chan struct{} // closed when Idle passed with no connection
	draining chan struct{} // closed on SIGTERM
	gone     chan struct{} // closed when the last connection ends, once stopped
	sessions sync.WaitGroup
}

func (d *daemon) run(ctx context.Context) error {
	// the sessions' own context: they outlive ctx while they drain
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.mu.Lock()
	d.timer = time.AfterFunc(Idle, d.idleFired)
	d.mu.Unlock()
	go d.accept(base)

	select {
	case <-ctx.Done():
	case <-d.idle:
		if d.stop() == 0 {
			return nil
		}
		// an agent came as the time ran out: let it finish
		select {
		case <-ctx.Done():
		case <-d.gone:
			return nil
		}
	}
	d.stop()
	d.drain()
	return nil
}

func (d *daemon) accept(ctx context.Context) {
	for {
		nc, err := d.l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond) // out of fds, say: try again
			continue
		}
		d.mu.Lock()
		if d.stopped {
			d.mu.Unlock()
			nc.Close()
			continue
		}
		d.n++
		if d.timer != nil {
			d.timer.Stop()
		}
		d.sessions.Add(1)
		d.mu.Unlock()
		go func() {
			defer d.sessions.Done()
			defer d.left()
			d.session(ctx, nc)
		}()
	}
}

func (d *daemon) left() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n--
	if d.n > 0 {
		return
	}
	if d.stopped {
		close(d.gone)
		return
	}
	d.timer = time.AfterFunc(Idle, d.idleFired)
}

func (d *daemon) idleFired() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.n == 0 && !d.stopped {
		select {
		case <-d.idle:
		default:
			close(d.idle)
		}
	}
}

// stop stops taking agents and says how many are still connected; gone
// closes when the last of them leaves.
func (d *daemon) stop() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.stopped {
		d.stopped = true
		if d.timer != nil {
			d.timer.Stop()
		}
		d.stopListening()
		d.gone = make(chan struct{})
		if d.n == 0 {
			close(d.gone)
		}
	}
	return d.n
}

// drain lets the calls in flight finish (at most Drain), then ends every
// connection. Waiting for a question isn't a call in flight: it ends at once.
func (d *daemon) drain() {
	d.mu.Lock()
	close(d.draining)
	for c := range d.conns {
		c.stop()
	}
	d.mu.Unlock()
	limit := time.After(Drain)
wait:
	for {
		d.mu.Lock()
		n := d.pending
		d.mu.Unlock()
		if n <= 0 {
			break
		}
		select {
		case <-d.changed:
		case <-limit:
			break wait
		}
	}
	d.mu.Lock()
	for c := range d.conns {
		c.end()
	}
	d.mu.Unlock()
	done := make(chan struct{})
	go func() { d.sessions.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second): // a call stuck past Drain dies with the process
	}
}

func (d *daemon) isDraining() bool {
	select {
	case <-d.draining:
		return true
	default:
		return false
	}
}

func (d *daemon) inflight(delta int) {
	d.mu.Lock()
	d.pending += delta
	d.mu.Unlock()
	select {
	case d.changed <- struct{}{}:
	default:
	}
}

// refuse turns calls away once the daemon is stopping: they never start,
// so the agent can safely make them again.
func (d *daemon) refuse(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method == "tools/call" && d.isDraining() {
			return nil, errors.New("darshan is restarting: nothing was done; try again in a moment")
		}
		return next(ctx, method, req)
	}
}

// session runs one agent's MCP session on nc, as that agent's Origin.
func (d *daemon) session(ctx context.Context, nc net.Conn) {
	defer nc.Close()
	br := bufio.NewReaderSize(nc, maxHello)
	o, err := greet(nc, br)
	if err != nil {
		return
	}
	c := newConn(d, nc, br)
	d.mu.Lock()
	if d.isDraining() {
		d.mu.Unlock()
		return
	}
	d.conns[c] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.conns, c)
		d.mu.Unlock()
		c.forget()
	}()
	srv := newServer(d.version, &server{origin: o, env: environ(o), halt: c.halt})
	srv.AddReceivingMiddleware(d.refuse)
	ss, err := srv.Connect(ctx, &mcp.IOTransport{Reader: c, Writer: c}, nil)
	if err != nil {
		return
	}
	_ = ss.Wait()
}
