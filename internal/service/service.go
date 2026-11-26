// Package service installs darshan's systemd user units: darshan-mcp.socket,
// where AI agents reach the MCP server through pranali, and darshan.service,
// which runs `darshan serve` when the socket is first used.
package service

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed darshan-mcp.socket
var socketUnit string

//go:embed darshan.service
var serviceUnit string

// Socket and Unit are the units' names.
const (
	Socket = "darshan-mcp.socket"
	Unit   = "darshan.service"
)

// Units are the two units, with exe as darshan's path.
func Units(exe string) map[string]string {
	q := strings.ReplaceAll(exe, "%", "%%")
	if strings.ContainsAny(q, " \t\"\\") {
		q = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(q) + `"`
	}
	return map[string]string{
		Socket: socketUnit,
		Unit:   strings.ReplaceAll(serviceUnit, "@DARSHAN@", q),
	}
}

// Write puts the units in dir.
func Write(dir, exe string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, body := range Units(exe) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// UserDir is where a user's own units live.
func UserDir() string {
	c := os.Getenv("XDG_CONFIG_HOME")
	if c == "" {
		h, _ := os.UserHomeDir()
		c = filepath.Join(h, ".config")
	}
	return filepath.Join(c, "systemd", "user")
}

// Exe is the path the units should run: the darshan found on PATH when it
// is this binary (a stable link, as kosha and install.sh make), else this
// binary itself.
func Exe() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := exec.LookPath("darshan"); err == nil {
		if a, err := filepath.Abs(p); err == nil {
			if fa, err := os.Stat(a); err == nil {
				if fs, err := os.Stat(self); err == nil && os.SameFile(fa, fs) {
					return a, nil
				}
			}
		}
	}
	return self, nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install writes the units for the user, listens on the socket, and
// restarts a running darshan serve so it is the binary just installed.
func Install(exe string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("no systemctl: without systemd, darshan mcp serves each agent in its own process")
	}
	if err := Write(UserDir(), exe); err != nil {
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", Socket}, {"try-restart", Unit}} {
		if err := systemctl(args...); err != nil {
			return err
		}
	}
	return nil
}

// Uninstall stops and removes the user's units.
func Uninstall() error {
	_ = systemctl("disable", "--now", Socket)
	_ = systemctl("stop", Unit)
	for _, name := range []string{Socket, Unit} {
		if err := os.Remove(filepath.Join(UserDir(), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return systemctl("daemon-reload")
}
