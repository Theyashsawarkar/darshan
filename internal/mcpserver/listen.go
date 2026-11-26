package mcpserver

// Where `darshan serve` listens: the socket systemd hands it
// (darshan-mcp.socket), else one it binds itself (dev runs, tests).

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Socket is where agents reach the daemon, by pranali's rules:
// $PRANALI_SOCKET, else $XDG_RUNTIME_DIR/darshan/mcp.sock, else
// ${XDG_STATE_HOME:-~/.local/state}/darshan/run/mcp.sock. Never /tmp.
// With DARSHAN_HOME (a separate darshan, for tests) and no PRANALI_SOCKET
// it is $DARSHAN_HOME/run/mcp.sock: that darshan's own daemon, which
// `darshan mcp` then names to pranali through PRANALI_SOCKET.
func Socket() string {
	if p := os.Getenv("PRANALI_SOCKET"); p != "" {
		return p
	}
	if h := os.Getenv("DARSHAN_HOME"); h != "" {
		return filepath.Join(h, "run", "mcp.sock")
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "darshan", "mcp.sock")
	}
	st := os.Getenv("XDG_STATE_HOME")
	if st == "" {
		h, _ := os.UserHomeDir()
		st = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(st, "darshan", "run", "mcp.sock")
}

// SocketName is the socket's FileDescriptorName systemd passes (the unit's
// name by default), looked for when it passes more than one.
const SocketName = "darshan-mcp.socket"

// listenFD is the fd systemd passed in env (LISTEN_PID, LISTEN_FDS,
// LISTEN_FDNAMES), or -1 when this process wasn't socket-activated.
func listenFD(getenv func(string) string, pid int) (int, error) {
	if p, err := strconv.Atoi(getenv("LISTEN_PID")); err != nil || p != pid {
		return -1, nil
	}
	n, err := strconv.Atoi(getenv("LISTEN_FDS"))
	if err != nil || n < 1 {
		return -1, nil
	}
	if n == 1 {
		return 3, nil
	}
	for i, name := range strings.Split(getenv("LISTEN_FDNAMES"), ":") {
		if name == SocketName && i < n {
			return 3 + i, nil
		}
	}
	return -1, fmt.Errorf("systemd passed %d sockets and none is %s", n, SocketName)
}

// activated takes the listener systemd passed, if any. The LISTEN_*
// variables go either way, so nothing the daemon starts inherits them.
func activated() (net.Listener, error) {
	fd, err := listenFD(os.Getenv, os.Getpid())
	for _, k := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
		_ = os.Unsetenv(k)
	}
	if fd < 0 || err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	f := os.NewFile(uintptr(fd), SocketName)
	defer f.Close() // FileListener has its own copy
	l, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("the socket systemd passed: %w", err)
	}
	return l, nil
}

// errRunning means another daemon answers on the socket.
var errRunning = errors.New("already running")

// listen returns the daemon's listener and what stops it: systemd's (the
// socket stays, connections queue for the next start), else one bound at
// Socket(), unlinked on stop only while it is still the one bound here.
func listen() (net.Listener, func(), error) {
	if l, err := activated(); l != nil || err != nil {
		return l, func() { _ = l.Close() }, err
	}
	return bind(Socket())
}

func bind(sock string) (net.Listener, func(), error) {
	if len(sock) > 107 {
		return nil, nil, fmt.Errorf("socket path too long for a unix socket (%d bytes, at most 107): %s", len(sock), sock)
	}
	dir := filepath.Dir(sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, nil, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || !ok || int(st.Uid) != os.Getuid() {
		return nil, nil, fmt.Errorf("%s is not a folder of this user's", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, nil, err
		}
	}
	if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
		c.Close()
		return nil, nil, errRunning
	} else if fi, lerr := os.Lstat(sock); lerr == nil {
		// something is there and nobody answers: a socket left by a daemon
		// that died goes; anything else stays
		if fi.Mode()&os.ModeSocket == 0 || !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, nil, fmt.Errorf("%s is in the way: %w", sock, err)
		}
		_ = os.Remove(sock)
	}
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", sock)
	syscall.Umask(old)
	if errors.Is(err, syscall.EADDRINUSE) {
		return nil, nil, errRunning
	}
	if err != nil {
		return nil, nil, err
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	mine, _ := os.Lstat(sock)
	stop := func() {
		_ = l.Close()
		if now, err := os.Lstat(sock); err == nil && mine != nil && os.SameFile(mine, now) {
			_ = os.Remove(sock)
		}
	}
	return l, stop, nil
}
