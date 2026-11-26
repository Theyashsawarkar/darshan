# Changelog

Newest first. Releases are tags (`vX.Y.Z`) with `Version` in cmd/darshan.

## Unreleased

- `darshan mcp` is a thin pipe now: every agent shares one `darshan serve`,
  started on demand on `$XDG_RUNTIME_DIR/darshan/mcp.sock`, which leaves after
  a minute with no agent and steps down when a new darshan is installed. Each
  pipe costs about 3 MB instead of 12-27 MB, and skips loading the screen's
  Markdown and highlighting code. Each agent keeps its own session, name, tmux
  pane and folder (relative paths in `view_open` start there). When the server
  can't be reached, `darshan mcp` serves in process as before.
- The pipe outlives its server: when `darshan serve` goes away mid-session,
  waiting calls get "darshan server restarted; try again" and the pipe starts
  a new server, replays the session's start and goes on, without the agent
  reconnecting. The server runs each agent's commands from that agent's
  `PATH`, and drops `LD_PRELOAD`, `LD_LIBRARY_PATH` and `CLAUDE*`,
  `ANTHROPIC*`, `MCP_*` variables from its own environment. `darshan mcp`
  with options says on stderr that it serves in process.

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
