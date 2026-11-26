package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// conn is one agent's connection, as the MCP session's reader and writer.
// It keeps the ids of the requests read and not yet answered, so that
//   - when the agent half-closes (pranali at its stdin's end), the answers
//     still owed are written before the session ends (half-open), and a
//     view_next_question stops waiting at once (halt);
//   - on SIGTERM the daemon knows when the calls in flight are done.
type conn struct {
	d  *daemon
	nc net.Conn
	br *bufio.Reader

	line []byte // the rest of the line being read

	mu      sync.Mutex
	pending map[string]bool
	changed chan struct{}

	halt      chan struct{} // closed: stop waiting for questions
	haltOnce  sync.Once
	abort     chan struct{} // closed: end now
	abortOnce sync.Once
}

// leaveGrace is how long a half-closed connection's calls get to answer.
var leaveGrace = 10 * time.Second

func newConn(d *daemon, nc net.Conn, br *bufio.Reader) *conn {
	return &conn{d: d, nc: nc, br: br, pending: map[string]bool{}, changed: make(chan struct{}, 1),
		halt: make(chan struct{}), abort: make(chan struct{})}
}

func (c *conn) stop() { c.haltOnce.Do(func() { close(c.halt) }) }

// end closes the connection now, answers owed or not.
func (c *conn) end() {
	c.stop()
	c.abortOnce.Do(func() {
		close(c.abort)
		_ = c.nc.Close()
	})
}

func (c *conn) Read(p []byte) (int, error) {
	if len(c.line) == 0 {
		line, err := c.br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			// the agent is gone, or only stopped writing: answer what's owed
			c.stop()
			c.drained(time.After(leaveGrace))
			return 0, err
		}
		c.track(line, true)
		c.line = line
	}
	n := copy(p, c.line)
	c.line = c.line[n:]
	return n, nil
}

func (c *conn) Write(p []byte) (int, error) {
	n, err := c.nc.Write(p)
	c.track(p, false)
	return n, err
}

func (c *conn) Close() error {
	c.end()
	return nil
}

// drained waits until every request read has its answer, until done
// fires or until the connection ends.
func (c *conn) drained(done <-chan time.Time) {
	for {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-c.changed:
		case <-done:
			return
		case <-c.abort:
			return
		}
	}
}

// forget drops what's still owed, once the session has ended.
func (c *conn) forget() {
	c.mu.Lock()
	n := len(c.pending)
	c.pending = map[string]bool{}
	c.mu.Unlock()
	c.d.inflight(-n)
}

// track notes the requests in msg (read from the agent: in) and the
// answers to them (written to it).
func (c *conn) track(msg []byte, in bool) {
	msg = bytes.TrimSpace(msg)
	type idMsg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	var msgs []idMsg
	if len(msg) > 0 && msg[0] == '[' {
		_ = json.Unmarshal(msg, &msgs)
	} else {
		var one idMsg
		if json.Unmarshal(msg, &one) == nil {
			msgs = append(msgs, one)
		}
	}
	delta := 0
	c.mu.Lock()
	for _, m := range msgs {
		key := idKey(m.ID)
		switch {
		case key == "":
		case in && m.Method != "": // the agent's request
			if !c.pending[key] {
				c.pending[key] = true
				delta++
			}
		case !in && m.Method == "": // our answer
			if c.pending[key] {
				delete(c.pending, key)
				delta--
			}
		}
	}
	c.mu.Unlock()
	if delta != 0 {
		select {
		case c.changed <- struct{}{}:
		default:
		}
		c.d.inflight(delta)
	}
}

// idKey names a JSON-RPC id, numbers and strings apart; "" for none.
func idKey(raw json.RawMessage) string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch v := v.(type) {
	case string:
		return "s" + v
	case float64:
		return fmt.Sprintf("n%v", v)
	}
	return ""
}
