// Package xdg finds darshan's folders. It imports nothing beyond os and
// path/filepath, so internal/mcppipe can use it before the heavy packages
// initialise (see there).
package xdg

import (
	"os"
	"path/filepath"
)

// Home is darshan's folder under the XDG variable env (default ~/def), or
// $DARSHAN_HOME for all of them.
func Home(env, def string) string {
	if h := os.Getenv("DARSHAN_HOME"); h != "" {
		return h
	}
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, "darshan")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, def, "darshan")
}

// StateDir holds state.json, the question queue and serve.log.
func StateDir() string { return Home("XDG_STATE_HOME", ".local/state") }
