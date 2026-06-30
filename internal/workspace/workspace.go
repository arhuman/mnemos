// Package workspace resolves the single MNEMOS_DIR anchor and the fixed layout
// derived from it. Every location mnemos uses — config, knowledge base, index
// database, embedding models — is a fixed subpath of MNEMOS_DIR; none is
// individually configurable. This replaces the earlier model where a relative
// [storage].path and [capture].dir were resolved against a config-derived tree
// root.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// Fixed names within a MNEMOS_DIR. The knowledge base (kb) is the single URI
// namespace and write-confinement root; capture lives inside it. State (the
// index database) and models live alongside kb but outside the URI namespace.
const (
	// DirName is the project-local anchor directory discovered by walking up
	// from the cwd (bounded by the git root).
	DirName = ".mnemos"
	// ConfigName is the config file inside a MNEMOS_DIR.
	ConfigName = "mnemos.toml"
	// CaptureName is the reserved subdirectory of the knowledge base where
	// auto-named notes (remember) are written. Its URIs are "capture/<file>".
	CaptureName = "capture"

	kbDir     = "kb"
	stateDir  = "state"
	modelsDir = "models"
	dbName    = "index.db"
)

// Layout is the set of absolute paths derived from a MNEMOS_DIR. It is the single
// source of truth for where mnemos reads and writes.
type Layout struct {
	// MnemosDir is the resolved anchor (absolute).
	MnemosDir string
	// Config is the mnemos.toml path. Usually MnemosDir/mnemos.toml; an explicit
	// --config overrides just this filename.
	Config string
	// KB is the knowledge base root: the tree root, URI namespace, and write
	// confinement boundary. Everything URI-addressable lives under it.
	KB string
	// Capture is where auto-named notes (remember) are written: KB/capture.
	Capture string
	// DB is the SQLite index: MnemosDir/state/index.db — derived state, outside
	// the URI namespace.
	DB string
	// Models is the embedding-model directory: MnemosDir/models.
	Models string
	// Source describes how MnemosDir was resolved, for status output and logs.
	Source string
}

// New builds a Layout from a MNEMOS_DIR by applying the fixed subpath rules. The
// directory is cleaned but not required to exist (init creates it).
func New(mnemosDir string) Layout {
	md := filepath.Clean(mnemosDir)
	kb := filepath.Join(md, kbDir)

	return Layout{
		MnemosDir: md,
		Config:    filepath.Join(md, ConfigName),
		KB:        kb,
		Capture:   filepath.Join(kb, CaptureName),
		DB:        filepath.Join(md, stateDir, dbName),
		Models:    filepath.Join(md, modelsDir),
	}
}

// Options carries the inputs to Resolve. The environment, home, and cwd are
// injected so resolution is deterministic and testable.
type Options struct {
	// ExplicitDir is the value of --mnemos-dir (highest precedence after config).
	ExplicitDir string
	// ConfigPath is the value of --config: an explicit mnemos.toml path. Its
	// directory becomes MnemosDir and it overrides the config filename.
	ConfigPath string
	// Env reads an environment variable (os.Getenv in production).
	Env func(string) string
	// Home is the user's home directory (os.UserHomeDir in production).
	Home string
	// Cwd is the current working directory (os.Getwd in production).
	Cwd string
}

// Resolve determines the MNEMOS_DIR and its Layout by precedence:
//
//  1. --config <file>      → MnemosDir is the file's directory; Config is the file
//  2. --mnemos-dir <dir>   → that directory
//  3. $MNEMOS_DIR          → that directory
//  4. project ./.mnemos    → nearest one walking up from cwd, bounded by the git
//     root (so an unrelated parent's .mnemos is never picked up)
//  5. ~/.mnemos            → the global default
//
// The chosen directory need not exist; commands that read state fail with an
// actionable error when the database is absent.
func Resolve(opts Options) (Layout, error) {
	env := opts.Env
	if env == nil {
		env = func(string) string { return "" }
	}

	if opts.ConfigPath != "" {
		abs, err := filepath.Abs(opts.ConfigPath)
		if err != nil {
			return Layout{}, fmt.Errorf("workspace: resolve --config %q: %w", opts.ConfigPath, err)
		}
		l := New(filepath.Dir(abs))
		l.Config = abs
		l.Source = "--config " + opts.ConfigPath

		return l, nil
	}

	switch {
	case opts.ExplicitDir != "":
		abs, err := filepath.Abs(opts.ExplicitDir)
		if err != nil {
			return Layout{}, fmt.Errorf("workspace: resolve --mnemos-dir %q: %w", opts.ExplicitDir, err)
		}
		l := New(abs)
		l.Source = "--mnemos-dir"

		return l, nil

	case env("MNEMOS_DIR") != "":
		abs, err := filepath.Abs(env("MNEMOS_DIR"))
		if err != nil {
			return Layout{}, fmt.Errorf("workspace: resolve $MNEMOS_DIR %q: %w", env("MNEMOS_DIR"), err)
		}
		l := New(abs)
		l.Source = "$MNEMOS_DIR"

		return l, nil
	}

	if proj := findProjectDir(opts.Cwd); proj != "" {
		l := New(proj)
		l.Source = "project " + filepath.Join(".", DirName)

		return l, nil
	}

	if opts.Home == "" {
		return Layout{}, fmt.Errorf("workspace: cannot resolve MNEMOS_DIR: no --config/--mnemos-dir/$MNEMOS_DIR, no project %s, and no home directory", DirName)
	}
	l := New(filepath.Join(opts.Home, DirName))
	l.Source = "default ~/" + DirName

	return l, nil
}

// findProjectDir walks up from cwd looking for a directory that contains a
// .mnemos subdirectory, returning that subdirectory's absolute path. The search
// stops at the git root (a directory containing .git): a project's workspace must
// live within its own repository, never inherited from an unrelated ancestor.
// Returns "" when none is found.
func findProjectDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, DirName)
		if isDir(candidate) {
			return candidate
		}
		if exists(filepath.Join(dir, ".git")) {
			return "" // reached the repo root without finding .mnemos
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isDir(p string) bool {
	info, err := os.Stat(p)

	return err == nil && info.IsDir()
}

func exists(p string) bool {
	_, err := os.Stat(p)

	return err == nil
}
