package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The units follow pranali's socket template and run this darshan.
func TestUnits(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "/home/u/my bin/dar%shan"); err != nil {
		t.Fatal(err)
	}
	sock, _ := os.ReadFile(filepath.Join(dir, Socket))
	for _, l := range []string{"ListenStream=%t/darshan/mcp.sock", "SocketMode=0600", "DirectoryMode=0700", "Service=darshan.service", "WantedBy=sockets.target"} {
		if !strings.Contains(string(sock), l+"\n") {
			t.Errorf("socket unit lacks %s", l)
		}
	}
	svc, _ := os.ReadFile(filepath.Join(dir, Unit))
	for _, l := range []string{`ExecStart="/home/u/my bin/dar%%shan" serve`, "Type=simple", "TimeoutStopSec=30", "Requires=darshan-mcp.socket"} {
		if !strings.Contains(string(svc), l+"\n") {
			t.Errorf("service unit lacks %s:\n%s", l, svc)
		}
	}
	if u := Units("/usr/bin/darshan")[Unit]; !strings.Contains(u, "ExecStart=/usr/bin/darshan serve\n") {
		t.Error(u)
	}
}
