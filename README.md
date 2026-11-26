<p align="center">
  <img alt="darshan" src="assets/logo.svg" width="120">
</p>

<!-- identity -->
<p align="center">
  <b>न तु मां शक्यसे द्रष्टुमनेनैव स्वचक्षुषा ।<br>
  दिव्यं ददामि ते चक्षुः पश्य मे योगमैश्वरम् ॥</b><br>
  <i>You cannot see me with these eyes alone; I give you an eye that can. Look.</i><br>
  <sub>Bhagavad Gītā 11.8</sub>
</p>
<!-- /identity -->

<p align="center">
  <b>PDFs, images and Markdown in your terminal.</b><br>
  Vim keys, a search across the whole system with a live preview, and a line to the AI agent that opened the file.
</p>

<p align="center">
  <img alt="Searching for a file: the results on the left, a live preview of the PDF on the right" src="assets/screenshots/search.png" width="100%">
</p>
<p align="center">
  <img alt="A page in dark mode, the colours inverted for reading at night" src="assets/screenshots/reading.png" width="49%">
  <img alt="Zen mode: nothing but the page, at full screen" src="assets/screenshots/zen.png" width="49%">
</p>
<p align="center"><sub>Screenshots use made-up files.</sub></p>

> *Darshan* (दर्शन) is Sanskrit for "seeing": a view of something, taken in
> properly.

**darshan** opens in a tmux popup, inside your work. Close it and your file
stays open, on the same page, until you say otherwise. Press one key and it
fills the screen with nothing but the page. Ask an agent to show you a document
and you can question it from right there: your question reaches the agent with
the page you're on, and its answer shows beside the page.

## Contents

- [Features](#features)
- [Install](#install)
- [Keys](#keys)
- [AI agents](#ai-agents)
- [Options](#options)
- [How it draws](#how-it-draws)

## Features

- **Real pages and pictures** on kitty and Ghostty, inside tmux and its popups
  too; coloured blocks on other terminals. Markdown is drawn as styled text.
- **Vim keys**: `j k h l`, `gg G`, counts (`5j`, `12G`), `Ctrl+d`/`Ctrl+u`, `/`
  with `n`/`N`, `:` for commands (a page number, `q`, `bd`, `e FILE`).
- **Search the whole system**: `f` finds any PDF, image or Markdown file by
  name, with a live preview of the one you're on. Recent files come first.
- **The popup remembers**: `prefix v` opens it and closes it. Your file and page
  come back until you close the file with `Space b d`.
- **Zen mode**: `z` switches between the normal popup and the full screen with
  nothing but the page.
- **Dark mode**: `Space u i` inverts pages and pictures, for reading at night.
- **It follows the file**: when a file changes, it reloads and keeps your page.
- **AI agents**: an MCP server lets any agent show you a file and talk about it.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/Theyashsawarkar/darshan/main/install.sh | bash
```

Or `make install`, or with TPM: `set -g @plugin 'Theyashsawarkar/darshan'`,
which binds `prefix v`. It needs **poppler** (`pdftoppm`, `pdfinfo`,
`pdftotext`) for PDFs and **fd** for the search; `darshan doctor` checks. tmux
needs `set -g allow-passthrough on` only for other tools: darshan writes
straight to the client's terminal.

To make it the desktop's viewer, install `packaging/darshan.desktop` and
`xdg-mime default darshan.desktop application/pdf image/png image/jpeg`.
`darshan open FILE` shows a file in the running screen, a tmux popup, or from
the desktop a kitty window of its own.

| Command | Does |
|---|---|
| `darshan [FILE] [-p N]` | The screen, on FILE or what you left open |
| `darshan open FILE [-p N]` | Show FILE from anywhere |
| `darshan popup` | Open the tmux popup, or close it when it's open |
| `darshan search WORDS [--kind pdf]` | Find files by name: one path a line |
| `darshan status` | What's open |
| `darshan close` | Close the open file |
| `darshan mcp` | The MCP server, for AI agents (a pipe to the one `darshan serve` they share) |
| `darshan doctor` | Check what it needs |

## Keys

darshan uses the vim grammar every Vayu tool shares
([vyakarana](https://github.com/Theyashsawarkar/vyakarana)): on the page
you are in NORMAL (keys are commands; a number repeats a motion: `5j`,
`12G`), the finder, the `/` search and the talk are typing. `Space` is the
leader: press it and a box lists what can follow (`Space u` toggles the
view, `Space b` the file, `Space o` opens it elsewhere, `Space k` asks
the agent). `Space Space` searches every action, `?` shows every key.
Inside the tmux popup its own key (`@darshan-key`, `v` by default) closes
it, like pressing it again outside.

`Space` no longer pages down: `Ctrl+f` does. Keys that moved say where
they went for one release: `a` → `Space k`, `i` → `Space u i`, `w` →
`Space u w`, `x` → `Space b d`.

<!-- keys: generated from the keymap; run the tests with -update -->
**Everywhere**

| Keys | What it does |
|---|---|
| <kbd>j</kbd> or <kbd>Down</kbd> | Move down (the next page once the page is all shown; 5j repeats) |
| <kbd>k</kbd> or <kbd>Up</kbd> | Move up (the previous page once the page is all shown) |
| <kbd>h</kbd> or <kbd>Left</kbd> | Move left when zoomed (else the previous page) |
| <kbd>l</kbd> or <kbd>Right</kbd> | Move right when zoomed (else the next page) |
| <kbd>J</kbd> or <kbd>PgDn</kbd> | Move to the next page |
| <kbd>K</kbd> or <kbd>PgUp</kbd> | Move to the previous page |
| <kbd>Ctrl+d</kbd> | Scroll half a screen down |
| <kbd>Ctrl+u</kbd> | Scroll half a screen up |
| <kbd>Ctrl+f</kbd> | Scroll a screen down (Space did this before it became the leader) |
| <kbd>Ctrl+b</kbd> | Scroll a screen up |
| <kbd>g</kbd> <kbd>g</kbd> or <kbd>Home</kbd> | Jump to the first page (5gg: page 5) |
| <kbd>G</kbd> or <kbd>End</kbd> | Jump to the last page (5G: page 5) |
| <kbd>:</kbd> or <kbd>Space</kbd> <kbd>g</kbd> <kbd>p</kbd> | Jump to a page (or line) by number: :12 (also :q, :bd, :e FILE) |
| <kbd>/</kbd> | Search the document's text |
| <kbd>n</kbd> | Jump to the next match |
| <kbd>N</kbd> | Jump to the previous match |
| <kbd>+</kbd> | Grow the zoom |
| <kbd>-</kbd> | Shrink the zoom |
| <kbd>=</kbd> | Reset to fit the whole page |
| <kbd>[</kbd> or <kbd>Space</kbd> <kbd>b</kbd> <kbd>p</kbd> | Switch to the previous file in the folder |
| <kbd>]</kbd> or <kbd>Space</kbd> <kbd>b</kbd> <kbd>n</kbd> | Switch to the next file in the folder |
| <kbd>f</kbd> or <kbd>o</kbd> or <kbd>Ctrl+p</kbd> or <kbd>Space</kbd> <kbd>f</kbd> <kbd>f</kbd> | Search for a file on the whole system (with a preview) |
| <kbd>z</kbd> or <kbd>Space</kbd> <kbd>u</kbd> <kbd>z</kbd> | Toggle zen mode: the full screen, nothing but the page |
| <kbd>r</kbd> or <kbd>Space</kbd> <kbd>r</kbd> | Reload the file (it also reloads by itself when it changes) |
| <kbd>y</kbd> or <kbd>Space</kbd> <kbd>y</kbd> <kbd>p</kbd> | Copy the file's path |
| <kbd>?</kbd> or <kbd>Space</kbd> <kbd>?</kbd> | Show every key |
| <kbd>q</kbd> or <kbd>Space</kbd> <kbd>q</kbd> | Close the popup; the file stays open for next time (the popup's own key too) |
| <kbd>Space</kbd> <kbd>Space</kbd> | Search every action and run one (the palette) |
| <kbd>Space</kbd> <kbd>b</kbd> | *File…* |
| <kbd>Space</kbd> <kbd>b</kbd> <kbd>d</kbd> | Close the file (the popup then finds another) |
| <kbd>Space</kbd> <kbd>k</kbd> | Open the talk with the agent that opened this file, to ask it something |
| <kbd>Space</kbd> <kbd>o</kbd> | *Open…* |
| <kbd>Space</kbd> <kbd>o</kbd> <kbd>b</kbd> | Open the file in the desktop's own viewer (xdg-open) |
| <kbd>Space</kbd> <kbd>o</kbd> <kbd>e</kbd> | Open the file in $EDITOR (Markdown) |
| <kbd>Space</kbd> <kbd>u</kbd> | *Toggles…* |
| <kbd>Space</kbd> <kbd>u</kbd> <kbd>i</kbd> | Invert the colours of pages and pictures (dark mode) |
| <kbd>Space</kbd> <kbd>u</kbd> <kbd>w</kbd> | Toggle fitting the page's width |
| | *While typing* |
| <kbd>Enter</kbd> | Run the search or the : command |
| <kbd>Esc</kbd> | Cancel the search or the : command |

**The file finder**

| Keys | What it does |
|---|---|
| <kbd>Enter</kbd> | Open the picked file |
| <kbd>Tab</kbd> or <kbd>Shift+Tab</kbd> | Switch between all files, PDFs, images and Markdown |
| | *While typing* |
| <kbd>Ctrl+j</kbd> or <kbd>Down</kbd> or <kbd>Ctrl+n</kbd> | Move to the next result while typing |
| <kbd>Ctrl+k</kbd> or <kbd>Up</kbd> or <kbd>Ctrl+p</kbd> | Move to the previous result while typing |
| <kbd>Esc</kbd> | Leave typing (NORMAL: j, k, g g, G, Ctrl+d / Ctrl+u move; i, a or / type again; Esc or q closes) |

**The talk with the agent**

| Keys | What it does |
|---|---|
| | *While typing* |
| <kbd>Enter</kbd> | Send your question, with what's on screen, to the agent |
| <kbd>Esc</kbd> | Close the talk |
<!-- /keys -->

## AI agents

Add it once and every agent can use it:

```sh
claude mcp add darshan -- darshan mcp
```

| Tool | Does |
|---|---|
| `view_open` | Show the user a file (`path`, `page`, `why`) |
| `view_search` | Find files anywhere on the system by name |
| `view_status` | What's open, the page, the text on their screen |
| `view_next_question` | Wait for the user's next question |
| `view_reply` | Show an answer beside the page |
| `view_close` | Close the file |

**Asking.** When an agent opened the file, `Space k` opens the talk beside the page.
Your question goes with the file, the page and the text on your screen: to the
agent if it's waiting in `view_next_question`, else typed straight into its tmux
pane, so it arrives like a message from you. If that pane has dropped to a shell
darshan won't type there; the question waits until the agent next asks for it.
The document's text is marked as data, so words inside a PDF can't pose as your
instructions. If the agent regenerates the file, it reloads where you were.

**One server for every agent.** `darshan mcp` is only a pipe (about 3 MB):
it passes the agent's MCP to one shared `darshan serve`, which it starts when
none runs, on `$XDG_RUNTIME_DIR/darshan/mcp.sock`. Each agent keeps its own
session: its name, its tmux pane and its folder go with it, so questions still
reach the right pane. The server leaves after a minute with no agent, and makes
way for a newly installed darshan. If it can't be reached, `darshan mcp` serves
in its own process as before (and says so on stderr; `serve.log` in the state
folder has the server's errors).

## Options

`~/.config/darshan/config.toml`:

```toml
roots   = ["~/more-books"]   # folders to search, beside home, other disks and system docs
exclude = ["archive"]        # folder names to skip, beside node_modules, .git, .cache and the like
```

tmux options, before TPM's `run` line:

| Option | Default | |
|---|---|---|
| `@darshan-key` | `v` | The popup key after the prefix (`none` binds nothing) |
| `@darshan-width` | `90%` | Popup width |
| `@darshan-height` | `88%` | Popup height |
| `@darshan-title` | ` darshan ` | Popup title |
| `@darshan-launcher` | | Your own popup command, given display-popup's arguments |

Colours follow [varna](https://github.com/Theyashsawarkar/varna) when it's there.

## How it draws

PDF pages become PNGs through poppler's `pdftoppm` (cached in
`~/.cache/darshan`). They reach the terminal by kitty's graphics protocol,
written to the tmux client's own terminal, and show through Unicode placeholders,
which any text layout carries: that's why they work in popups, where viewers that
draw directly (tdf, for one) show nothing.

Not yet: highlighting a match on a PDF page (`/` jumps to the page), animated
GIFs (the first frame shows), pictures inside Markdown.
