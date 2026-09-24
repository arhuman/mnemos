package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
)

// TestTemporalRankingResolvesConfigAndFlag covers the interaction that decides
// whether a query is ranked by recency: the config supplies the standing
// preference, --recent turns it on for one query, and neither may weaken the
// other silently.
func TestTemporalRankingResolvesConfigAndFlag(t *testing.T) {
	tests := []struct {
		name         string
		cfg          config.SearchConfig
		recent       bool
		wantWeight   float64
		wantHalflife time.Duration
	}{
		{
			name:       "off by default",
			cfg:        config.SearchConfig{TemporalHalflife: "168h"},
			wantWeight: 0,
			// The engine substitutes its own default for a zero halflife, so the
			// parsed value is still carried even when the weight disables ranking.
			wantHalflife: 168 * time.Hour,
		},
		{
			name:         "--recent enables ranking when config leaves it off",
			cfg:          config.SearchConfig{TemporalHalflife: "168h"},
			recent:       true,
			wantWeight:   recentTemporalWeight,
			wantHalflife: 168 * time.Hour,
		},
		{
			name:         "a configured weight applies without the flag",
			cfg:          config.SearchConfig{TemporalWeight: 0.3, TemporalHalflife: "24h"},
			wantWeight:   0.3,
			wantHalflife: 24 * time.Hour,
		},
		{
			name:         "--recent never weakens a configured weight",
			cfg:          config.SearchConfig{TemporalWeight: 0.3, TemporalHalflife: "24h"},
			recent:       true,
			wantWeight:   0.3,
			wantHalflife: 24 * time.Hour,
		},
		{
			name:         "a malformed halflife falls back rather than failing the search",
			cfg:          config.SearchConfig{TemporalWeight: 1, TemporalHalflife: "not-a-duration"},
			wantWeight:   1,
			wantHalflife: 0,
		},
		{
			name:         "a non-positive halflife is rejected, not divided by",
			cfg:          config.SearchConfig{TemporalWeight: 1, TemporalHalflife: "0s"},
			wantWeight:   1,
			wantHalflife: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			weight, halflife := temporalRanking(tc.cfg, tc.recent)
			require.InDelta(t, tc.wantWeight, weight, 1e-9)
			require.Equal(t, tc.wantHalflife, halflife)
		})
	}
}
