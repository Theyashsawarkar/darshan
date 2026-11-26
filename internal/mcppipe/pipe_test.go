package mcppipe

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// lines collects what the pipe writes to the agent, a line at a time.
type lines struct {
	mu  sync.Mutex
	buf strings.Builder
	got chan map[string]any
}

func (l *lines) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(b)
	for {
		s := l.buf.String()
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return len(b), nil
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(s[:i]), &m)
		l.got <- m
		l.buf.Reset()
		l.buf.WriteString(s[i+1:])
	}
}

func (l *lines) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case m := <-l.got:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the agent")
		return nil
	}
}

// daemon is a fake darshan serve on one end of a net.Pipe.
type daemon struct {
	conn net.Conn
	r    *bufio.Reader
}

func (d daemon) read(t *testing.T) map[string]any {
	t.Helper()
	_ = d.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	l, err := d.r.ReadBytes('\n')
	if err != nil {
		t.Fatal("daemon read:", err)
	}
	var m map[string]any
	if err := json.Unmarshal(l, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (d daemon) answer(t *testing.T, id any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
	if _, err := d.conn.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

// start runs a pipe whose dials hand out fake daemons on daemons (or fail
// when dialFails is set); the agent writes to the returned writer.
func start(t *testing.T, dialFails *bool, dials *int) (io.WriteCloser, *lines, chan daemon, chan error) {
	daemons := make(chan daemon, 4)
	var mu sync.Mutex
	dial := func() (io.ReadWriteCloser, error) {
		mu.Lock()
		defer mu.Unlock()
		*dials++
		if *dialFails {
			return nil, errors.New("no daemon")
		}
		a, b := net.Pipe()
		daemons <- daemon{b, bufio.NewReader(b)}
		return a, nil
	}
	first, _ := dial()
	out := &lines{got: make(chan map[string]any, 16)}
	inR, inW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- newPipe(dial, out).run(inR, first) }()
	return inW, out, daemons, done
}

func say(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s+"\n"); err != nil {
		t.Fatal(err)
	}
}

func TestReconnectReplaysTheSession(t *testing.T) {
	fails, dials := false, 0
	in, out, daemons, done := start(t, &fails, &dials)
	old := <-daemons

	say(t, in, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"clientInfo":{"name":"claude-code"}}}`)
	if m := old.read(t); m["method"] != "initialize" {
		t.Fatal(m)
	}
	old.answer(t, 0)
	out.next(t)
	say(t, in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	old.read(t)
	say(t, in, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"view_next_question"}}`)
	old.read(t)
	old.conn.Close() // the daemon dies with a call in flight

	if m := out.next(t); m["id"] != 1.0 || m["error"].(map[string]any)["message"] != Restarted {
		t.Fatalf("in-flight call got %v", m)
	}
	fresh := <-daemons
	m := fresh.read(t)
	if m["method"] != "initialize" || m["id"] != "pipe-reinit-1" || m["params"].(map[string]any)["clientInfo"].(map[string]any)["name"] != "claude-code" {
		t.Fatalf("replayed %v", m)
	}
	fresh.answer(t, "pipe-reinit-1")
	if m := fresh.read(t); m["method"] != "notifications/initialized" {
		t.Fatal(m)
	}
	say(t, in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"view_status"}}`)
	if m := fresh.read(t); m["id"] != 2.0 {
		t.Fatal(m)
	}
	fresh.answer(t, 2)
	if m := out.next(t); m["id"] != 2.0 || m["result"] == nil {
		t.Fatalf("after the restart got %v (the reinit answer must not reach the agent)", m)
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNoDaemonForAWhile(t *testing.T) {
	old := ReconnectFor
	ReconnectFor = 300 * time.Millisecond
	t.Cleanup(func() { ReconnectFor = old })
	fails, dials := false, 0
	in, out, daemons, done := start(t, &fails, &dials)
	first := <-daemons
	fails = true
	first.conn.Close()
	time.Sleep(50 * time.Millisecond)

	say(t, in, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"view_status"}}`)
	if m := out.next(t); m["id"] != 7.0 || m["error"] == nil {
		t.Fatalf("queued call got %v", m)
	}
	before := dials
	fails = false
	say(t, in, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"view_status"}}`)
	d := <-daemons // the next request tries again
	if m := d.read(t); m["id"] != 8.0 {
		t.Fatal(m)
	}
	if dials <= before {
		t.Error("no new dial")
	}
	d.answer(t, 8)
	if m := out.next(t); m["id"] != 8.0 || m["result"] == nil {
		t.Fatal(m)
	}
	in.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipe didn't leave with the agent")
	}
}
