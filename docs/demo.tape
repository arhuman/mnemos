# docs/demo.tape — the mnemos hero demo.
#
# Render (from the repo root, with `mnemos` on PATH — see Makefile `make install`):
#   vhs docs/demo.tape        # writes docs/demo.gif
#
# Tuned to the vendored examples/onpage-seo/bundle corpus (a real OKF bundle), so
# it runs with no off-camera setup. The query "core web vitals" lands on a clean
# top citation (technical/core-web-vitals.md). See .claude/doc/screencast_demo.md.
#
# NOTE: keep typed `#` comment lines apostrophe-free — a typed `'` opens a shell
# quote (quote>) that swallows the command that follows.

Output docs/demo.gif

Set Shell "zsh"
Set FontSize 20
Set Width 1100
Set Height 700
Set Padding 22
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

# --- hidden setup: clean prompt, enter the corpus, reset prior state, clear ---
Hide
Type "setopt INTERACTIVE_COMMENTS" Enter
Type "export PROMPT='%F{cyan}mnemos ❯%f '" Enter
Type "cd examples/onpage-seo/bundle" Enter
Type "rm -rf .mnemos .mnemos.toml" Enter
Type "clear" Enter
Show

# --- Beat 1: the 'simplest' hook (zero setup) ---
Type "# one Go binary — no Python, no vector DB, no Docker, no cloud"
Enter
Sleep 900ms
Type "mnemos init"
Enter
Sleep 1000ms
Type "# point it at a real OKF bundle: plain markdown with frontmatter"
Enter
Sleep 700ms
Type "ls"
Enter
Sleep 900ms
Type "mnemos ingest . --collection seo"
Enter
Sleep 1600ms

# --- Beat 2: the payoff — a cited answer ---
Type "# ask a question — get the answer WITH its source"
Enter
Sleep 1100ms
Type `mnemos search "core web vitals" --collection seo`
Enter
Sleep 4000ms      # rest on the cited result — this is the loop's resting frame
