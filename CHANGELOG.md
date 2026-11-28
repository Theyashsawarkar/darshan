# Changelog

Newest first. Releases are tags (`vX.Y.Z`) with `Version` in cmd/darshan.

## Unreleased

- Website: pranali joins the family list in the footer; the site builder passes ruff.

## v0.3.0 -- 2026-11-27

- One MCP server for every agent, on the pranali protocol: `darshan mcp`
  execs [pranali](https://github.com/Theyashsawarkar/pranali) (`pranali darshan
  --fallback darshan mcp --in-process`), which carries the agent's MCP to one
  shared `darshan serve` on `$XDG_RUNTIME_DIR/darshan/mcp.sock`. Each agent
  keeps its own session, name, tmux pane, desktop, `PATH` and folder (relative
  paths in `view_open` start there). Without pranali, or with no server, it
  serves in process as before, with one line on stderr.
- systemd owns the server: `darshan-mcp.socket` and `darshan.service`, set up by
  the new `darshan service install` (install.sh and `make service` run it;
  `make install` puts the units in `$PREFIX/lib/systemd/user`). It starts on
  the first agent and leaves after a minute with none. An update restarts it:
  calls in flight get up to 20 s, a waiting `view_next_question` ends at once,
  new calls are turned away untried, and agents carry over. A running server
  never steps down for another build (the step-down of the unreleased pipe
  left agents from before an update failing every call), and it never removes
  a socket it didn't bind.
- Answers aren't lost when an agent stops writing: the server still sends
  what it owes on a half-closed connection.
- The server drops `LD_PRELOAD`, `LD_LIBRARY_PATH` and `CLAUDE*`,
  `ANTHROPIC*`, `MCP_*` variables from its own environment.
- Gone: the pipe inside darshan (internal/mcppipe), which started the server
  itself, kept it single with a lock, and ran from a package `init` before
  the rest of darshan loaded. The server still accepts that pipe's hello for
  this release, so agents started before the update keep working.

- The website, closer to Vayu's own: the tool's screenshot blurred behind the page, a centred hero with the logo and the verse, the page you're on as the white pill, glass "On this page" and footer panels, releases as cards. No glow behind the logo, no coloured line beside the verse.
- The website has Vayu's Spatial look: graphite glass, a floating navigation bar, near-white for links and the main button, and the tool's own colour only as its signature (the logo's glow, the shloka's line, the release dots). No more all-caps labels.

## v0.2.0 -- 2026-11-09

- Vim keys, the grammar every Vayu tool now shares (from the new shared
  module vyakarana): `Space` is the leader, with a which-key box after
  300 ms: `Space u` toggles the view (zen, dark, fit width), `Space b` the
  file (close, next, previous), `Space o` opens it elsewhere (the desktop's
  viewer, new; `$EDITOR` for Markdown, new), `Space k` asks the agent,
  `Space g p` goes to a page. `Space Space` searches every action; `?`
  scrolls and no longer closes on a stray key.
- `Space` no longer pages down: `Ctrl+f` does. Moved keys say where they
  went: `a` → `Space k`, `i` → `Space u i`, `w` → `Space u w`, `x` →
  `Space b d`.
- The popup closes with its own key (`@darshan-key`), not a hard-coded `v`;
  tmux spellings like `C-v` or `M-v` work too.
- Keys typed fast (`5j`, `gg`, `Space Space` in one go) count as separate
  keys: the terminal hands them over together.
- The README's key tables and `scripts/keybindings` are generated from the
  keymap; the search shows each key's view and mode (`darshan:normal`).
- The drawing kit (`internal/ui`) and kitty's diacritics come from
  vyakarana instead of copies.
- Every corner is rounded: the website's callouts.
- A logo and a shloka at the top of the README and the website: every Vayu tool has them now.

## v0.1.0 -- 2026-11-02

- PDFs, images and Markdown in the terminal, as real pictures on kitty (tmux
  popups too), with vim keys.
- Search for a file across the whole system, with a live preview.
- A tmux popup that keeps your file open until you close it, and zen mode.
- An MCP server: any agent can show you a file, and you can ask it about what
  you're reading from the viewer. It gets your question with the file, page and
  what's on screen, and its answer shows beside the page.
