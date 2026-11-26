package mcppipe

// The pipe outlives its daemon: when darshan serve goes away (a new
// install, a crash, a kill) while the agent is still there, the pipe
// answers what was left unanswered with an error, starts or finds a new
// daemon, replays the agent's hello, initialize and initialized, and goes
// on. The agent never sees the server restart, beyond those errors.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/Theyashsawarkar/darshan/internal/xdg"
)

// How long and how often the pipe tries to reach a daemon again, and how
// many messages wait meanwhile.
var (
	ReconnectFor = 60 * time.Second
	backoffMin   = 100 * time.Millisecond
	backoffMax   = 2 * time.Second
	queueMax     = 64
)

// Restarted is the error that answers requests the old daemon took with it.
const Restarted = "darshan server restarted; try again"

// Pipe joins stdin and stdout to the daemon, starting it if needed. It
// returns an error, before reading stdin, when no daemon can be reached at
// first; after that it keeps going until stdin closes.
func Pipe(exe string) error {
	h := Hello{Proto: Proto, Build: Build(), State: xdg.StateDir(), Origin: Here()}
	conn, err := Connect(h, exe)
	if err != nil {
		return err
	}
	dial := func() (io.ReadWriteCloser, error) {
		c, err := Connect(h, exe)
		if err != nil {
			return nil, err
		}
		return c, nil
	}
	return newPipe(dial, os.Stdout).run(os.Stdin, conn)
}

// message is what the pipe reads of a JSON-RPC line.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

func parse(line []byte) message {
	var m message
	_ = json.Unmarshal(line, &m)
	if string(m.ID) == "null" {
		m.ID = nil
	}
	return m
}

func (m message) request() bool { return m.Method != "" && m.ID != nil }

// key is an id as both sides spell it (1 and 1.0 are one id).
func key(id json.RawMessage) string {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return string(id)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

type link struct {
	rw io.ReadWriteCloser
	br *bufio.Reader
}

type pipe struct {
	dial func() (io.ReadWriteCloser, error) // Connect, hello and all

	outMu sync.Mutex
	out   io.Writer

	mu          sync.Mutex
	pending     map[string]json.RawMessage // requests sent, not yet answered
	initialize  []byte                     // the agent's, to replay
	initialized []byte
	reinits     int
}

func newPipe(dial func() (io.ReadWriteCloser, error), out io.Writer) *pipe {
	return &pipe{dial: dial, out: out, pending: map[string]json.RawMessage{}}
}

func (p *pipe) write(line []byte) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	_, _ = p.out.Write(line)
}

// fail answers the request id with Restarted.
func (p *pipe) fail(id json.RawMessage) {
	p.write(fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":%q}}`+"\n", id, Restarted))
}

func (p *pipe) failPending() {
	p.mu.Lock()
	ids := p.pending
	p.pending = map[string]json.RawMessage{}
	p.mu.Unlock()
	for _, id := range ids {
		p.fail(id)
	}
}

// run carries messages until stdin closes; first is the open connection.
func (p *pipe) run(in io.Reader, first io.ReadWriteCloser) error {
	lines := make(chan []byte)
	go func() {
		br := bufio.NewReader(in)
		for {
			l, err := br.ReadBytes('\n')
			if len(bytes.TrimSpace(l)) > 0 {
				if l[len(l)-1] != '\n' {
					l = append(l, '\n')
				}
				lines <- l
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	down := make(chan struct{}, 1)
	up := make(chan *link, 1)
	failed := make(chan struct{}, 1)
	cur := &link{first, bufio.NewReader(first)}
	go p.readServer(cur, down)
	var queue [][]byte
	reconnecting := false
	reconnect := func() {
		reconnecting = true
		go func() {
			if l := p.reconnect(); l != nil {
				up <- l
			} else {
				failed <- struct{}{}
			}
		}()
	}
	send := func(l []byte, m message) {
		if m.request() {
			p.mu.Lock()
			p.pending[key(m.ID)] = m.ID
			p.mu.Unlock()
		}
		_, _ = cur.rw.Write(l) // if it fails, readServer sees the end
	}
	for {
		select {
		case l, ok := <-lines:
			if !ok { // the agent left
				if cur != nil {
					cur.rw.Close()
				}
				return nil
			}
			m := parse(l)
			p.mu.Lock()
			switch m.Method {
			case "initialize":
				p.initialize = l
			case "notifications/initialized":
				p.initialized = l
			}
			p.mu.Unlock()
			switch {
			case cur != nil:
				send(l, m)
			case m.Method == "": // an answer to the old daemon: nobody wants it
			case len(queue) >= queueMax:
				if m.request() {
					p.fail(m.ID)
				}
			default:
				queue = append(queue, l)
				if !reconnecting {
					reconnect()
				}
			}
		case <-down:
			cur.rw.Close()
			cur = nil
			p.failPending()
			if !reconnecting {
				reconnect()
			}
		case l := <-up:
			reconnecting = false
			cur = l
			go p.readServer(cur, down)
			for _, q := range queue {
				send(q, parse(q))
			}
			queue = nil
		case <-failed: // nobody for ReconnectFor: try again on the next message
			reconnecting = false
			for _, q := range queue {
				if m := parse(q); m.request() {
					p.fail(m.ID)
				}
			}
			queue = nil
		}
	}
}

// readServer copies the daemon's lines to stdout, crossing off answered
// requests, and says when the daemon is gone.
func (p *pipe) readServer(l *link, down chan<- struct{}) {
	for {
		line, err := l.br.ReadBytes('\n')
		if err != nil { // a partial last line is dropped
			down <- struct{}{}
			return
		}
		if m := parse(line); m.Method == "" && m.ID != nil {
			p.mu.Lock()
			delete(p.pending, key(m.ID))
			p.mu.Unlock()
		}
		p.write(line)
	}
}

// reconnect reaches a daemon again within ReconnectFor, backing off, and
// replays the session's start; nil when none answers.
func (p *pipe) reconnect() *link {
	end := time.Now().Add(ReconnectFor)
	wait := backoffMin
	for {
		if rw, err := p.dial(); err == nil {
			if l, err := p.replay(rw); err == nil {
				return l
			}
			rw.Close()
		}
		if time.Now().Add(wait).After(end) {
			return nil
		}
		time.Sleep(wait)
		wait = min(wait*2, backoffMax)
	}
}

// replay sends the agent's initialize again under an id of the pipe's own,
// swallows its answer, and sends initialized: the new daemon's session is
// then where the old one was, with the agent's name.
func (p *pipe) replay(rw io.ReadWriteCloser) (*link, error) {
	l := &link{rw, bufio.NewReader(rw)}
	p.mu.Lock()
	init, inited := p.initialize, p.initialized
	p.reinits++
	n := p.reinits
	p.mu.Unlock()
	if init == nil {
		return l, nil
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(init, &req); err != nil {
		return nil, err
	}
	id := json.RawMessage(fmt.Sprintf(`"pipe-reinit-%d"`, n))
	req["id"] = id
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := rw.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	t := time.AfterFunc(5*time.Second, func() { rw.Close() })
	defer t.Stop()
	for {
		line, err := l.br.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		if m := parse(line); m.Method == "" && key(m.ID) == key(id) {
			break // anything before it came before initialize: not the agent's
		}
	}
	if inited != nil {
		if _, err := rw.Write(inited); err != nil {
			return nil, err
		}
	}
	return l, nil
}
