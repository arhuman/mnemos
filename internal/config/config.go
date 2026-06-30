// Package config defines the mnemos configuration schema, its built-in
// defaults, and the loader that merges those defaults with the single
// mnemos.toml found inside the active MNEMOS_DIR. The config carries behaviour
// only — indexing rules, chunking, search, the MCP surface, and security — never
// locations: every path is derived from the MNEMOS_DIR by package workspace.
package config

import (
	"fmt"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// Config mirrors mnemos.toml. It is loaded by overlaying the file (if present)
// on top of built-in defaults, so a missing section or key always falls back to
// a sane value.
type Config struct {
	Indexing IndexingConfig `koanf:"indexing"`
	Chunking ChunkingConfig `koanf:"chunking"`
	Search   SearchConfig   `koanf:"search"`
	MCP      MCPConfig      `koanf:"mcp"`
	Capture  CaptureConfig  `koanf:"capture"`
	Security SecurityConfig `koanf:"security"`
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

// CaptureConfig configures write-back behaviour. The capture location itself is
// not configurable: it is always the kb/capture directory of the active
// MNEMOS_DIR (see package workspace).
type CaptureConfig struct {
	// DeferToWatcher, when true, makes mnemos.remember write-only: the OKF file
	// is written but not ingested one-shot, leaving kb/capture ingestion to a
	// running watcher. Default false keeps the one-shot ingest, which is safe
	// even alongside a watcher because the Phase 1 hash-skip makes the watcher's
	// re-sighting of the file a no-op.
	DeferToWatcher bool `koanf:"defer_to_watcher"`
}

// SecurityConfig configures secret exclusion during ingestion.
type SecurityConfig struct {
	ExcludeSecrets bool     `koanf:"exclude_secrets"`
	Exclude        []string `koanf:"exclude"`
}

// defaultTOML holds the built-in configuration, identical to the file written by
// `mnemos init`. It is the single source of truth for defaults: the loader
// parses it first, then overlays the user file. It carries no location keys.
const defaultTOML = `[indexing]
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
// it to seed a new mnemos.toml.
func DefaultTOML() []byte {
	return []byte(defaultTOML)
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

// Load builds a Config from the embedded defaults, then overlays the single
// config file at path when it exists. A missing file is fine: the defaults stand
// on their own. A malformed file is an error. fileExists is the caller-supplied
// existence check so callers control filesystem semantics in tests.
func Load(path string, fileExists func(string) bool) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(rawbytes.Provider(DefaultTOML()), toml.Parser()); err != nil {
		return nil, fmt.Errorf("config: load defaults: %w", err)
	}

	if path != "" && fileExists(path) {
		if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
			return nil, fmt.Errorf("config: load %q: %w", path, err)
		}
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	return &cfg, nil
}
