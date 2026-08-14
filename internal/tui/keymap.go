package tui

// The editor binds plain key strings rather than a bubbles/key map: the set is
// small, flat, and every binding is handled in one switch.
const (
	keyQuit      = "q"
	keyQuitCtrl  = "ctrl+c"
	keySave      = "s"
	keyFocusNext = "tab"
	keyBack      = "b"
	keyBackspace = "backspace"
	keyEnter     = "enter"
	keyEscape    = "esc"
	keyEdit      = "e"
	keyMove      = "m"
	keyUp        = "up"
	keyUpVim     = "k"
	keyDown      = "down"
	keyDownVim   = "j"
	keyLeft      = "left"
	keyLeftVim   = "h"
	keyRight     = "right"
	keyRightVim  = "l"
	keyPageUp    = "pgup"
	keyPageDown  = "pgdown"
)

// helpLine is the always-visible key reference at the foot of the editor.
const helpLine = "tab focus · ↑↓ move · enter open/edit · ←→ cycle enum · e $EDITOR · m move/rename · s save · b back · q quit"

// isGateKey reports whether k is one of the keys that can be confirmed a second
// time to push past a failed save. Any other key cancels a pending confirmation,
// so "press again" always means the same action twice in a row.
func isGateKey(k string) bool {
	switch k {
	case keyQuit, keyQuitCtrl, keyEnter, keyBack, keyBackspace, keyMove:
		return true
	default:
		return false
	}
}
