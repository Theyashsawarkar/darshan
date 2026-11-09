package tui

import (
	"flag"
	"testing"

	"github.com/Theyashsawarkar/vyakarana/keymaptest"
)

var update = flag.Bool("update", false, "rewrite scripts/keybindings and the README's key tables")

// scripts/keybindings (Vayu's keybinding search) and the README's key
// tables must match Keys.
func TestKeybindingsFile(t *testing.T) {
	keymaptest.FileUpToDate(t, "../../scripts/keybindings", Keys.KeybindingsFile("Tmux", "go test ./internal/tui -update"), *update)
	keymaptest.SectionUpToDate(t, "../../README.md", Keys.Markdown(), *update)
}

// Every key follows the grammar: verb first, the reserved Space groups,
// no key that is also the start of a longer one.
func TestKeysFollowTheGrammar(t *testing.T) { keymaptest.All(t, Keys) }
