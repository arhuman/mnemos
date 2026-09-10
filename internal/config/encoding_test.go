package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
)

func TestEncodingRulesDefaultEmpty(t *testing.T) {
	cfg, err := config.Load("", missing)
	require.NoError(t, err)
	require.Empty(t, cfg.EncodingRules(), "ingest stays UTF-8-only until a charset is declared")
}

func TestEncodingRulesLoadInOrder(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", `
[[indexing.encoding]]
match = ["legacy/**"]
charset = "windows-1250"

[[indexing.encoding]]
match = ["**/*.dfm"]
charset = "cp1252"
`)

	cfg, err := config.Load(path, exists)
	require.NoError(t, err)

	rules := cfg.EncodingRules()
	require.Len(t, rules, 2)
	// Order is precedence (first match wins), so it must survive the load.
	require.Equal(t, []string{"legacy/**"}, rules[0].Match)
	require.Equal(t, "windows-1250", rules[0].Charset)
	require.Equal(t, "cp1252", rules[1].Charset)
}

// An unresolvable charset must fail at load, not per-file after a partial index.
func TestEncodingUnknownCharsetRejectedAtLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", `
[[indexing.encoding]]
match = ["**/*.pas"]
charset = "not-a-real-charset"
`)

	_, err := config.Load(path, exists)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not-a-real-charset")
}

// A rule with no globs would silently apply to nothing, which is never intended.
func TestEncodingEmptyMatchRejectedAtLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", `
[[indexing.encoding]]
match = []
charset = "windows-1250"
`)

	_, err := config.Load(path, exists)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no match globs")
}
