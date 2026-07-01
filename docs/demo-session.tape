# docs/demo-session.tape — the live Claude session (the payoff clip).
#
# Render (from the repo root):
#   vhs docs/demo-session.tape        # writes docs/demo-session.gif
#
# This is the ONE clip that is not deterministic: it drives a real `claude` call,
# so it makes a live API request and the answer differs every run. Treat it like a
# take — render a few times and keep the cleanest. Per the honesty guardrails in
# .claude/doc/screencast_demo.md, do NOT stage or fake the answer.
#
# It uses PRINT MODE (`claude -p`), not the interactive TUI. Claude Code's TUI
# renders in the alternate screen buffer, which is unreliable to drive under VHS
# (the prompt falls through to the shell). Print mode streams one answer to stdout
# and exits — deterministic to pace and immune to that problem.
#
# PREREQUISITES (read before rendering):
#   1. `claude` must be LOGGED IN (this tape uses your real Claude config).
#   2. You must NOT already have a `mnemos` MCP server registered at user scope; it
#      would collide with the local one this tape registers:
#         claude mcp remove mnemos -s user      # restore it afterwards
#   3. The EMBED build on PATH (`make install-embed`) plus the model
#      (`mnemos models install all-MiniLM-L6-v2`): this clip shows SEMANTIC search.
#   4. (Recommended) install the mnemos-okf skill so Claude reliably reaches for
#      memory:  make install-skill
#
# Corpus: the shipped OKF bundle in examples/git-recipes (six git recipes).

Output docs/demo-session.gif

Set Shell "zsh"
Set FontSize 20
Set Width 1100
Set Height 720
Set Padding 22
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

# --- hidden setup: index the real bundle, register mnemos as a TRUSTED local server ---
# Local scope = you added it, so Claude trusts it and shows no approval prompt.
# The absolute --config ($PWD/.mnemos.toml) is what makes the session actually use
# mnemos: claude launches the stdio server from an unspecified cwd, so a relative
# storage path would resolve to an empty database. mnemos resolves a relative
# [storage].path against the config file's directory.
# VHS does NOT wait for a typed command to finish before running the next line.
# Each slow setup step therefore needs an explicit Sleep: without them the
# still-streaming `ingest` / `claude mcp add` output bleeds past `clear` into the
# visible recording, and the Beat-1 comment gets typed into a busy shell (its head
# is eaten and the tail runs as a bogus command).
Hide
Type "setopt INTERACTIVE_COMMENTS" Enter
Type "export PROMPT='%F{cyan}mnemos ❯%f '" Enter
Type "cd examples/git-recipes/bundle" Enter
Type "rm -rf .mnemos .mnemos.toml" Enter
Sleep 500ms
Type "mnemos init && perl -pi -e 's/use_vectors = false/use_vectors = true/' .mnemos.toml" Enter
Sleep 1s
Type "mnemos ingest . --collection git && mnemos reindex --embeddings" Enter
Sleep 4s
Type "claude mcp remove mnemos -s local 2>/dev/null; claude mcp add mnemos -- mnemos serve --config $PWD/.mnemos.toml" Enter
Sleep 4s
Type "clear" Enter
Sleep 800ms
Show
Sleep 600ms

# --- Beat 1: ask Claude one question answered only in the project's OKF memory ---
# --allowedTools pre-authorizes the mnemos read tools so the one-shot call doesn't
# stall on a permission prompt. This is the SEMANTIC payoff: the question says
# "disappeared", a word that appears NOWHERE in the corpus, so lexical search finds
# nothing — but hybrid vector search bridges it to recovery/reflog.md ("recover lost
# commits") and Claude cites it. Requires the embed build + use_vectors = true.
# `< /dev/null` is essential under VHS: claude -p reads stdin (it supports piped
# input), and a pty's stdin never reaches EOF, so without the redirect the one-shot
# call blocks forever waiting for input. /dev/null gives it an immediate EOF.
# NOTE: keep this comment apostrophe-free — a typed `'` opens a shell quote
# (quote>) that swallows the command that follows.
Type "# semantic search: the notes never say the word disappeared"
Enter
Sleep 900ms
Type "claude -p 'How do I recover commits that disappeared? Answer from the project notes and cite the source.' --allowedTools mcp__mnemos__mnemos_search,mcp__mnemos__mnemos_context < /dev/null"
Enter

# --- Beat 2: Claude calls mnemos.search and answers WITH the citation ---
# The live one-shot call runs ~45s; Sleep out the streamed answer, then rest on the
# cited result — the frame that sells the tool. If a take cuts off, bump this.
Sleep 55s
Sleep 4s

# --- hidden trailer: leave the demo dir clean (claude -p already exited) ---
Hide
Type "claude mcp remove mnemos -s local 2>/dev/null" Enter
Sleep 2s
Type "rm -rf .mnemos .mnemos.toml" Enter
Sleep 500ms
Show
