// Package mcppipe is `darshan mcp`: a pipe between an agent's stdio and the
// one MCP daemon (`darshan serve`, internal/mcpserver) every agent shares,
// starting the daemon when none runs.
//
// Before MCP starts, the pipe sends one line, a Hello with its build, state
// folder and Origin (the agent's tmux server and pane, desktop, directory:
// what the daemon can't see itself), and the daemon answers one line.
//
// The pipe runs from this package's init, so it never pays for the screen's
// packages (chroma's lexers and styles, glamour, bluemonday: some 10 MB).
// That works because Go initialises packages in import-path order, each as
// soon as its imports are. "github.com/Theyashsawarkar/..." sorts before
// "github.com/alecthomas/...", "github.com/charmbracelet/..." and every
// other heavy module, so this package runs first, provided its imports are
// done by then: it uses only standard packages that sort early and need no
// vendor/ ones, and internal/xdg. Not net (it waits for vendor/golang.org/
// x/net, which sorts last): the socket is dialled with syscall. Not
// internal/state (it imports net). TestBinary/light checks it.
package mcppipe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Theyashsawarkar/darshan/internal/xdg"
)

// Proto names the hello's format.
const Proto = "darshan-mcp/1"

// Hello is the pipe's first line.
type Hello struct {
	Proto  string `json:"proto"`
	Build  string `json:"build"` // Build(): a daemon serves only its own binary
	State  string `json:"state"` // the state folder: one daemon serves one darshan home
	Origin Origin `json:"origin"`
}

// Answer is the daemon's first line.
type Answer struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Retry bool   `json:"retry,omitempty"` // this daemon has stepped down: start a new one
}

// Origin is where one agent's session runs: what used to come from the
// process itself when every agent ran its own darshan mcp.
type Origin struct {
	Pid int               `json:"pid,omitempty"`
	Cwd string            `json:"cwd,omitempty"` // relative paths start here
	Env map[string]string `json:"env,omitempty"` // OriginEnv's variables that are set
}

// OriginEnv are the variables that say where an agent runs: its tmux
// server and pane, and its desktop (for a window of its own outside tmux).
var OriginEnv = []string{"TMUX", "TMUX_PANE", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY"}

// Here is this process's own origin.
func Here() Origin {
	o := Origin{Pid: os.Getpid(), Env: map[string]string{}}
	o.Cwd, _ = os.Getwd()
	for _, k := range OriginEnv {
		if v, ok := os.LookupEnv(k); ok {
			o.Env[k] = v
		}
	}
	return o
}

// Build names this binary: its path, size and time. A new install is a
// new build, and an old daemon steps down for it.
func Build() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return exe
	}
	return fmt.Sprintf("%s %d %d", exe, fi.Size(), fi.ModTime().UnixNano())
}

// SocketDir holds the daemon's socket and lock: $XDG_RUNTIME_DIR/darshan,
// or the state folder with DARSHAN_HOME (tests) or no runtime dir.
func SocketDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && os.Getenv("DARSHAN_HOME") == "" {
		return filepath.Join(d, "darshan")
	}
	return xdg.StateDir()
}

// Socket is where the daemon listens.
func Socket() string { return filepath.Join(SocketDir(), "mcp.sock") }

// Err is why the pipe that ran at start couldn't reach a daemon; then
// main serves MCP in process.
var Err error

func init() {
	if len(os.Args) != 2 || os.Args[1] != "mcp" {
		return
	}
	exe, err := os.Executable()
	if err == nil {
		if err = Pipe(exe); err == nil {
			os.Exit(0)
		}
	}
	Err = err
}

// Pipe joins stdin and stdout to the daemon, starting it if needed. It
// returns an error, before reading stdin, when no daemon can be reached.
func Pipe(exe string) error {
	conn, err := Connect(Hello{Proto: Proto, Build: Build(), State: xdg.StateDir(), Origin: Here()}, exe)
	if err != nil {
		return err
	}
	defer conn.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		done <- struct{}{} // the agent left: end the session
	}()
	go func() {
		_, _ = io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()
	<-done
	return nil
}

// Connect greets the daemon as h, starting `exe serve` when nobody
// answers, and returns the connection, ready for MCP.
func Connect(h Hello, exe string) (*os.File, error) {
	line, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}
	line = append(line, '\n')
	var last error
	for range 2 { // twice: once more after an old daemon stepped down
		conn, err := dial(exe)
		if err != nil {
			return nil, err
		}
		a, err := handshake(conn, line)
		if err == nil && a.OK {
			return conn, nil
		}
		conn.Close()
		if err != nil {
			last = err // it went away as we came: start another
			continue
		}
		last = errors.New(a.Error)
		if !a.Retry {
			break
		}
	}
	return nil, last
}

func handshake(conn *os.File, line []byte) (Answer, error) {
	var a Answer
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(line); err != nil {
		return a, err
	}
	// byte by byte: nothing after the answer may be read here
	var got []byte
	b := make([]byte, 1)
	for {
		if _, err := conn.Read(b); err != nil {
			return a, err
		}
		if b[0] == '\n' {
			break
		}
		if got = append(got, b[0]); len(got) > 4096 {
			return a, errors.New("darshan serve: answer too long")
		}
	}
	_ = conn.SetDeadline(time.Time{})
	return a, json.Unmarshal(got, &a)
}

// dialUnix connects to the unix socket at path, as net.Dial would.
func dialUnix(path string) (*os.File, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		syscall.Close(fd)
		return nil, &os.PathError{Op: "dial", Path: path, Err: err}
	}
	// non-blocking: the runtime's poller then serves it, with deadlines
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// dial connects to the daemon, starting it when nobody answers.
func dial(exe string) (*os.File, error) {
	if c, err := dialUnix(Socket()); err == nil {
		return c, nil
	}
	if err := spawn(exe); err != nil {
		return nil, err
	}
	var err error
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		var c *os.File
		if c, err = dialUnix(Socket()); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("darshan serve didn't start: %w", err)
}

// spawn starts `darshan serve` on its own, in a session of its own, so it
// outlives the agent that started it; its errors go to serve.log.
func spawn(exe string) error {
	if err := os.MkdirAll(xdg.StateDir(), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(xdg.StateDir(), "serve.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	c := exec.Command(exe, "serve")
	c.Dir = "/"
	c.Stdout, c.Stderr = log, log
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }() // reap it if it exits while we run
	return nil
}
