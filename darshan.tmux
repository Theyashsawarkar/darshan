#!/usr/bin/env bash
# darshan as a tmux plugin: TPM entry point (also works with a plain
# `run-shell` on this file). Binds prefix + @darshan-key to open darshan in a
# popup, or close it when it's open. Closing keeps the file: the same key
# brings it back, where you left it, until you close it with x.
#
# Options (set them before TPM's `run` line):
#   @darshan-key       key after the prefix (default v; "none" binds nothing)
#   @darshan-width     popup width  (default 90%)
#   @darshan-height    popup height (default 88%)
#   @darshan-title     popup title  (default " darshan ")
#   @darshan-launcher  your own popup command instead of display-popup: it is
#                      run with display-popup's arguments appended (-E -T
#                      TITLE CMD), tmux formats expanded, e.g.
#                      "~/.tmux/scripts/popup.sh '#{client_name}'"
#
# Zen mode (z in darshan) reopens the popup at full size.

dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
run="$dir/scripts/darshan-run"
key=$(tmux show-option -gqv @darshan-key)
key=${key:-v}
[ "$key" = none ] && exit 0
tmux bind-key -N "Toggle darshan popup (PDFs, images, Markdown; keeps your file open)" "$key" \
  "run -b \"'$run' popup --client '#{client_name}'\""
