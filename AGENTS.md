# darshan -- notes for AI agents (same as CLAUDE.md)

PDFs, images and Markdown in the terminal: one Go binary (`cmd/darshan`) with
a Bubble Tea screen, a CLI and an MCP server. Usable as the `darshan` command
(install.sh, make install, kosha) and as a TPM plugin (`darshan.tmux` binds
the popup key). It must work on any Linux setup without Vayu: no hardcoded
paths, Vayu-specific behaviour only through the `@darshan-*` options.

- Layout: `internal/tui` the screen (`model.go` reading, `find.go` the search,
  `render.go` pages to PNGs, `kitty.go` graphics); `internal/doc` PDFs through
  poppler, images, Markdown; `internal/index` the system-wide file list (fd);
  `internal/state` what's remembered, the control socket, popups and questions
  to agents; `internal/mcpserver` the agent tools.
- Pictures go to the terminal with kitty's graphics protocol, written to the
  tmux client's own tty, and show through Unicode placeholders: the only way
  that works in tmux popups (tdf, for one, draws nothing there).
- One screen at a time: it listens on a unix socket; `darshan open`, the popup
  key and the MCP tools talk to it. Closing keeps state.json's file; only x
  closes it.
- A question goes to `view_next_question` if an agent is waiting, else it's
  typed into the agent's tmux pane, never when that pane has dropped to a
  shell. Document text always travels marked as data.
- Keys live in `internal/tui/keys.go`. After a change: `go test ./internal/tui
  -update` (rewrites `scripts/keybindings`), and update the README's key table.
  Descriptions start with a verb from Vayu's keybind-search VERBS.
- Every change gets a CHANGELOG.md entry and the README documents every
  feature, key and option; releases are tags with `Version` in cmd/darshan
  bumped. `make check` must pass.
- Test in a headless sway with kitty and a throwaway tmux, with `DARSHAN_HOME`
  and `DARSHAN_ROOTS` set, never on the user's screen. Public screenshots use
  made-up files only.
- The website (docs/, GitHub Pages) is generated: after changing README.md,
  CHANGELOG.md or docs/adr/, run `python3 scripts/build-site.py` and commit
  docs/ (CI checks it).
