package tui

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite scripts/keybindings")

// scripts/keybindings (Vayu's keybinding search) must match Keys.
func TestKeybindingsFile(t *testing.T) {
	const path = "../../scripts/keybindings"
	want := KeybindingsFile()
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatal("scripts/keybindings is out of date: go test ./internal/tui -update")
	}
}

// Every key is in the README.
func TestReadmeHasEveryKey(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range Keys {
		if !strings.Contains(string(b), k.Desc) {
			t.Errorf("README lacks %q (%s)", k.Desc, k.Keys)
		}
	}
}
