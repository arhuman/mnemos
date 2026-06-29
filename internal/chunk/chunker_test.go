package chunk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
)

// TestConfigFrom verifies ConfigFrom maps the token counts onto Config fields.
func TestConfigFrom(t *testing.T) {
	cc := chunk.ConfigFrom(700, 80)
	require.Equal(t, 700, cc.TargetTokens)
	require.Equal(t, 80, cc.OverlapTokens)
}
