package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
)

// TestSecurityExcludeDisabled verifies that SecurityExclude returns nil when
// ExcludeSecrets is false, regardless of what the Exclude slice contains.
func TestSecurityExcludeDisabled(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			ExcludeSecrets: false,
			Exclude:        []string{"**/.env", "*.key"},
		},
	}
	require.Nil(t, cfg.SecurityExclude())
}

// TestSecurityExcludeEnabled verifies that SecurityExclude returns the
// configured glob list when ExcludeSecrets is true.
func TestSecurityExcludeEnabled(t *testing.T) {
	globs := []string{"**/.env", "*.pem", "*.key"}
	cfg := &config.Config{
		Security: config.SecurityConfig{
			ExcludeSecrets: true,
			Exclude:        globs,
		},
	}
	require.Equal(t, globs, cfg.SecurityExclude())
}

// TestConfinementExcludeIgnoresExcludeSecrets verifies that the write/delete
// confinement guard always sees the security globs, even when ExcludeSecrets is
// off. Disabling secret exclusion only widens what is indexed; it must never
// loosen which paths a write/delete tool may touch.
func TestConfinementExcludeIgnoresExcludeSecrets(t *testing.T) {
	globs := []string{"**/.env", "*.pem", "*.key"}
	for _, excludeSecrets := range []bool{true, false} {
		cfg := &config.Config{
			Security: config.SecurityConfig{
				ExcludeSecrets: excludeSecrets,
				Exclude:        globs,
			},
		}
		require.Equal(t, globs, cfg.ConfinementExclude(),
			"ConfinementExclude must return the globs regardless of ExcludeSecrets=%v", excludeSecrets)
	}
}
