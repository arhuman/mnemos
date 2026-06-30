// Package config defines the mnemos configuration schema, its built-in
// defaults, and the layered loader that merges those defaults with the user's
// home- and project-level TOML files.
package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// FileName is the conventional config filename, auto-discovered in the user's
// home directory and the current working directory.
const FileName = ".mnemos.toml"

// Config mirrors .mnemos.toml. It is loaded by layering the user files (if
// present) on top of built-in defaults, so a missing section or key always
// falls back to a sane value.
type Config struct {
	Storage  StorageConfig  `koanf:"storage"`
	Indexing IndexingConfig `koanf:"indexing"`
	Chunking ChunkingConfig `koanf:"chunking"`
	Search   SearchConfig   `koanf:"search"`
	MCP      MCPConfig      `koanf:"mcp"`
	Capture  CaptureConfig  `koanf:"capture"`
	Security SecurityConfig `koanf:"security"`
}

// StorageConfig configures on-disk persistence.
type StorageConfig struct {
	Path string `koanf:"path"`
}

// IndexingConfig configures which files are discovered for ingestion.
type IndexingConfig struct {
	Include []string `koanf:"include"`
	Exclude []string `koanf:"exclude"`
	// MaxFileBytes caps the size of a single file read into memory during
	// ingestion. A matched file larger than this is skipped with a warning
	// rather than read whole, which bounds memory under the parallel pipeline.
	// 0 (or negative) disables the cap.
	MaxFileBytes int64 `koanf:"max_file_bytes"`
}

// ChunkingConfig configures deterministic chunk sizing.
type ChunkingConfig struct {
	TargetTokens  int `koanf:"target_tokens"`
	OverlapTokens int `koanf:"overlap_tokens"`
}

// SearchConfig configures retrieval defaults.
type SearchConfig struct {
	DefaultLimit int  `koanf:"default_limit"`
	UseVectors   bool `koanf:"use_vectors"`
}

// MCPConfig configures the MCP server surface.
type MCPConfig struct {
	Transport  string `koanf:"transport"`
	AllowWrite bool   `koanf:"allow_write"`
	// AllowDelete gates the destructive tree operations mnemos.forget and
	// mnemos.move (and their CLI counterparts). It is distinct from AllowWrite
	// so capture can be enabled without granting delete/move. Default false.
	AllowDelete bool `koanf:"allow_delete"`
}

// CaptureConfig configures the write-back capture directory.
type CaptureConfig struct {
	Dir string `koanf:"dir"`
	// DeferToWatcher, when true, makes mnemos.remember write-only: the OKF file
	// is written but not ingested one-shot, leaving capture_dir ingestion to a
	// running watcher. Default false keeps the one-shot ingest, which is safe
	// even alongside a watcher because Phase 1 hash-skip makes the watcher's
	// re-sighting of the file a no-op.
	DeferToWatcher bool `koanf:"defer_to_watcher"`
}

// SecurityConfig configures secret exclusion during ingestion.
type SecurityConfig struct {
	ExcludeSecrets bool     `koanf:"exclude_secrets"`
	Exclude        []string `koanf:"exclude"`
}

// defaultTOML holds the built-in configuration, identical to the file written
// by `mnemos init`. It is the single source of truth for defaults: the loader
// parses it first, then overlays the user files.
const defaultTOML = `[storage]
path = ".mnemos/mnemos.db"

[indexing]
include = ["**/*.md", "**/*.txt", "**/*.go", "**/*.sql"]
exclude = [".git/**", "node_modules/**", "vendor/**", "dist/**"]
# Skip any single file larger than this many bytes (default 4 MiB). Bounds
# memory during the parallel scan; set to 0 to disable the cap.
max_file_bytes = 4194304

[chunking]
target_tokens = 700
overlap_tokens = 80

[search]
default_limit = 12
use_vectors = false

[mcp]
transport = "stdio"
allow_write = false
allow_delete = false

[capture]
dir = ".mnemos/capture"
defer_to_watcher = false

[security]
exclude_secrets = true
exclude = [
  "**/.env",
  "**/*.pem",
  "**/*.key",
  "**/id_rsa",
  "**/secrets/**",
]
`

// DefaultTOML returns the canonical default configuration document. `init` uses
// it to seed a new .mnemos.toml.
func DefaultTOML() []byte {
	return []byte(defaultTOML)
}

// Validate checks the location-bearing config against the tree root. It is
// called once at load time (app.Load) so every command shares the same check
// rather than each rediscovering it. Currently it validates [capture].dir, which
// must resolve within the tree root (an absolute value is accepted as long as it
// stays inside; a relative value is anchored to the root).
func (c *Config) Validate(treeRoot string) error {
	if _, _, err := c.CaptureLocation(treeRoot); err != nil {
		return err
	}

	return nil
}

// CaptureLocation resolves [capture].dir against the tree root and returns the
// absolute directory to write auto-named notes into and the tree-root-relative
// directory used to derive their citation URIs. An absolute capture.dir is
// accepted when it stays within the tree root; a relative one is anchored to the
// root (not the process cwd), so capture lands in the same place regardless of
// where a command — or an MCP server — was launched. A capture.dir that escapes
// the tree root is rejected, since its notes could not carry a tree-root-relative
// URI.
func (c *Config) CaptureLocation(treeRoot string) (absDir, relDir string, err error) {
	rootAbs, err := filepath.Abs(treeRoot)
	if err != nil {
		return "", "", fmt.Errorf("config: resolve tree root %q: %w", treeRoot, err)
	}
	rootAbs = filepath.Clean(rootAbs)

	dir := c.Capture.Dir
	if filepath.IsAbs(dir) {
		absDir = filepath.Clean(dir)
	} else {
		absDir = filepath.Clean(filepath.Join(rootAbs, dir))
	}

	rel, err := filepath.Rel(rootAbs, absDir)
	if err != nil {
		return "", "", fmt.Errorf("config: relativize [capture].dir %q against tree root %q: %w", dir, treeRoot, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("config: [capture].dir %q escapes the tree root %q; set it inside the tree", dir, rootAbs)
	}

	return absDir, rel, nil
}

// ConfinementExclude returns the globs the write/delete confinement guard
// enforces on caller-supplied paths (remember custom path, forget, move, okfy).
// Unlike SecurityExclude it is NOT gated by exclude_secrets: these globs define
// paths a write/delete tool may never touch, a boundary that must hold
// regardless of whether secrets are also being kept out of the index.
func (c *Config) ConfinementExclude() []string {
	return c.Security.Exclude
}

// SecurityExclude returns the indexing-time secret-exclusion globs, gated by
// exclude_secrets: turning that off means "index everything", so the globs no
// longer remove matching files from the scan and an empty set is returned.
func (c *Config) SecurityExclude() []string {
	if !c.Security.ExcludeSecrets {
		return nil
	}

	return c.Security.Exclude
}

// Resolve determines the ordered config files to layer and the writable tree
// root, given the explicit --config value (empty means auto-discover) and the
// user's home directory.
//
// Precedence is expressed by order: later files override earlier ones. When
// explicitPath is set it REPLACES auto-discovery and wins outright; its
// directory becomes the tree root. Otherwise the home file (~/.mnemos.toml) is
// layered first and the project file (./.mnemos.toml) overrides it, with the
// current directory as the tree root.
func Resolve(explicitPath, homeDir string) (paths []string, treeRoot string) {
	if explicitPath != "" {
		return []string{explicitPath}, filepath.Dir(explicitPath)
	}

	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, FileName))
	}
	paths = append(paths, FileName)

	return paths, "."
}

// Load builds a Config from the embedded defaults, then overlays each existing
// file in paths in order (later files win). Missing files are skipped: the
// defaults stand on their own. A malformed file is an error. fileExists is the
// caller-supplied existence check so callers control filesystem semantics in
// tests.
func Load(paths []string, fileExists func(string) bool) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(rawbytes.Provider(DefaultTOML()), toml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load defaults: %w", err)
	}

	for _, p := range paths {
		if p == "" || !fileExists(p) {
			continue
		}
		if err := k.Load(file.Provider(p), toml.Parser()); err != nil {
			return nil, fmt.Errorf("config: load %q: %w", p, err)
		}
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	return &cfg, nil
}
