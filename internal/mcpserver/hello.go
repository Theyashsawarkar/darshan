package mcpserver

// The first line of every connection to `darshan serve`: the pranali hello
// (docs: github.com/Theyashsawarkar/pranali, docs/PROTOCOL.md), or the
// hello of darshan 0.2's own pipe, still accepted for one release so the
// pipes of sessions started before the update keep working.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/Theyashsawarkar/darshan/internal/state"
)

// Origin is where one agent's session runs: what used to come from the
// process itself when every agent ran its own darshan mcp.
type Origin struct {
	Pid   int               `json:"pid,omitempty"`
	Cwd   string            `json:"cwd,omitempty"` // relative paths start here
	Env   map[string]string `json:"env,omitempty"` // OriginEnv's variables that are set
	Agent string            `json:"-"`             // pranali's --agent label
}

// OriginEnv are the variables that say where an agent runs: its tmux
// server and pane, its desktop (for a window of its own outside tmux), and
// the PATH its commands (tmux, kitty) are found on. They are the six
// pranali's hello carries.
var OriginEnv = []string{"TMUX", "TMUX_PANE", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "PATH"}

// here is this process's own origin, for the in-process server.
func here() Origin {
	o := Origin{Pid: os.Getpid(), Env: map[string]string{}}
	o.Cwd, _ = os.Getwd()
	for _, k := range OriginEnv {
		if v, ok := os.LookupEnv(k); ok {
			o.Env[k] = v
		}
	}
	return o
}

// legacyProto names the hello of darshan 0.2's pipe (internal/mcppipe).
const legacyProto = "darshan-mcp/1"

// hello is either hello: pranali's fields at the top, the legacy one's
// under proto/build/state/origin. Unknown fields are ignored.
type hello struct {
	Pranali int               `json:"pranali"`
	Tool    string            `json:"tool"`
	Agent   string            `json:"agent"`
	Pid     int               `json:"pid"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`

	Proto  string  `json:"proto"`
	State  string  `json:"state"`
	Origin *Origin `json:"origin"`
	// "build" is ignored: a daemon serves every build (updates restart it
	// through systemd), so a pipe from before an update keeps working
}

// answer is the daemon's line; legacy pipes read ok and error from it too.
type answer struct {
	Pranali int    `json:"pranali"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// helloTimeout is how long a connection has to say hello.
const helloTimeout = 5 * time.Second

// maxHello bounds the hello line.
const maxHello = 64 << 10

// greet reads the hello from br (nc's reader), answers it on nc and
// returns the agent's origin, or an error after answering ok:false.
func greet(nc net.Conn, br *bufio.Reader) (Origin, error) {
	_ = nc.SetDeadline(time.Now().Add(helloTimeout))
	defer func() { _ = nc.SetDeadline(time.Time{}) }()
	line, err := br.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		err = errors.New("hello too long")
		reply(nc, err)
		return Origin{}, err
	}
	if err != nil {
		return Origin{}, err
	}
	o, err := parseHello(line)
	reply(nc, err)
	return o, err
}

func reply(w io.Writer, err error) {
	a := answer{Pranali: 1, OK: err == nil}
	if err != nil {
		a.Error = "darshan serve: " + err.Error()
	}
	_ = json.NewEncoder(w).Encode(a)
}

func parseHello(line []byte) (Origin, error) {
	var h hello
	if err := json.Unmarshal(line, &h); err != nil {
		return Origin{}, errors.New("not a pranali hello")
	}
	var o Origin
	switch {
	case h.Pranali != 0:
		if h.Pranali != 1 {
			return o, fmt.Errorf("speaks pranali 1, not %d", h.Pranali)
		}
		if h.Tool != "darshan" {
			return o, fmt.Errorf("this is darshan, not %q", h.Tool)
		}
		o = Origin{Pid: h.Pid, Cwd: h.Cwd, Env: h.Env, Agent: h.Agent}
	case h.Proto == legacyProto:
		// one darshan home per daemon: the legacy pipe says which
		if h.State != "" && h.State != state.Dir() {
			return o, errors.New("keeps its state in " + state.Dir() + ", not " + h.State)
		}
		if h.Origin != nil {
			o = *h.Origin
		}
	default:
		return o, errors.New("not a pranali hello")
	}
	env := map[string]string{}
	for _, k := range OriginEnv {
		if v, ok := o.Env[k]; ok {
			env[k] = v
		}
	}
	o.Env = env
	return o, nil
}
