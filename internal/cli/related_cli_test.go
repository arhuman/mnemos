package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// relatedNeighborJSON mirrors one entry of the `related --json` neighbor lists.
type relatedNeighborJSON struct {
	URI       string `json:"uri"`
	Direction string `json:"direction"`
	Resolved  bool   `json:"resolved"`
}

// TestRelatedCLIListsOutboundAndInbound ingests two linked documents and
// asserts `related` reports the outbound link from a.md and the matching
// inbound backlink on b.md, in both tabular and JSON form.
func TestRelatedCLIListsOutboundAndInbound(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "a.md", "# A\n\nlinks to [b](b.md)\n")
	seedKB(t, "b.md", "# B\n\nno outbound links here\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runCmd(t, "related", "a.md")
	require.Contains(t, out, "DIRECTION")
	require.Contains(t, out, "outbound")
	require.Contains(t, out, "b.md")

	out = runCmd(t, "related", "b.md")
	require.Contains(t, out, "inbound")
	require.Contains(t, out, "a.md")

	jsonOut := runCmd(t, "related", "b.md", "--json")
	var res struct {
		URI      string                `json:"uri"`
		Outbound []any                 `json:"outbound"`
		Inbound  []relatedNeighborJSON `json:"inbound"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &res))
	require.Equal(t, "b.md", res.URI)
	require.Empty(t, res.Outbound)
	require.Len(t, res.Inbound, 1)
	require.Equal(t, "a.md", res.Inbound[0].URI)
	require.True(t, res.Inbound[0].Resolved)
}

// TestRelatedCLINoNeighbors asserts a document with no links prints the plain
// "no neighbors" message.
func TestRelatedCLINoNeighbors(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "lonely.md", "# Lonely\n\nno links at all\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runCmd(t, "related", "lonely.md")
	require.Equal(t, "no neighbors\n", out)
}

// TestRelatedCLIUnknownURI asserts an unresolvable uri surfaces as an error
// rather than an empty neighborhood.
func TestRelatedCLIUnknownURI(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	_, err := runCmdErr(t, "related", "nope.md")
	require.Error(t, err)
}
