# docs/demo-mcp.tape — the "wire it into Claude Code" clip.
#
# Render (from the repo root, with `mnemos` AND `claude` on PATH):
#   vhs docs/demo-mcp.tape        # writes docs/demo-mcp.gif
#
# Deterministic companion to demo.tape: it registers the MCP server at PROJECT
# scope, which writes a committable .mcp.json, then shows that file — the single
# artifact you check in so Claude Code picks mnemos up automatically. It does NOT
# fake a chat answer, nor claim a live "connected" check: a project-scoped server
# is pending approval until the user accepts it in-session, so a green check can't
# be shown honestly in an unattended render. The real answer is the separately-
# recorded live session (see .claude/doc/screencast_demo.md). Tuned to the
# examples/onpage-seo corpus (a vendored real OKF bundle) so the server has
# something real to serve.
#
# NOTE: `claude mcp add -s project` writes .mcp.json in the working dir; the hidden
# setup and trailer both clear any prior registration so re-runs start clean.
#
# The `--mnemos-dir .mnemos` arg is RELATIVE on purpose: a project-scoped .mcp.json
# is committed and must stay portable, and Claude Code launches a project server from
# the directory holding .mcp.json — so the relative MNEMOS_DIR resolves there, and the
# kb, index db, and models all derive from it. (The live session clip uses an absolute
# --mnemos-dir instead, because a local-scope server is launched from an unspecified
# cwd; see demo-session.tape.)

Output docs/demo-mcp.gif

Set Shell "zsh"
Set FontSize 20
Set Width 1100
Set Height 700
Set Padding 22
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

# --- hidden setup: clean prompt, enter the corpus, index it, clear ---
# VHS does NOT wait for a typed command to finish, so each slow setup step needs
# an explicit Sleep — otherwise the still-streaming `mnemos ingest` output bleeds
# past `clear` into the visible recording and the Beat-1 comment is typed into a
# busy shell.
Hide
Type "setopt INTERACTIVE_COMMENTS" Enter
Type "export PROMPT='%F{cyan}mnemos ❯%f '" Enter
Type "cd examples/onpage-seo" Enter
Type "rm -rf .mnemos .mcp.json" Enter
Sleep 500ms
Type "claude mcp remove -s project mnemos 2>/dev/null" Enter
Sleep 2s
Type "mnemos init && mnemos add bundle --collection seo" Enter
Sleep 3s
Type "clear" Enter
Sleep 800ms
Show
Sleep 600ms

# --- Beat 1: wire mnemos into Claude Code in one command ---
Type "# give Claude Code a local, cited memory — one file, no cloud"
Enter
Sleep 900ms
Type "claude mcp add -s project mnemos -- mnemos serve --mnemos-dir .mnemos"
Enter
Sleep 3s

# --- Beat 2: the payoff — a committable, project-scoped config file ---
Type "# commit this — Claude Code picks it up automatically"
Enter
Sleep 900ms
Type "cat .mcp.json"
Enter
Sleep 3500ms      # rest on the .mcp.json — this is the loop's resting frame

# --- Beat 3: name the tools Claude can now call on its own ---
Type "# Claude now calls these automatically: search · read · context · remember"
Enter
Sleep 2500ms

# --- hidden trailer: unregister so the demo dir is left clean ---
Hide
Type "claude mcp remove -s project mnemos 2>/dev/null" Enter
Sleep 2s
Type "rm -rf .mnemos .mcp.json" Enter
Sleep 500ms
Show
