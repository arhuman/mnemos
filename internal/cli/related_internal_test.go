package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/memory"
)

func TestRenderRelatedEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderRelated(&buf, memory.RelatedResult{URI: "a.md"})
	require.Equal(t, "no neighbors\n", buf.String())
}

func TestRenderRelatedListsOutboundAndInbound(t *testing.T) {
	res := memory.RelatedResult{
		URI: "a.md",
		Outbound: []memory.RelatedNeighbor{
			{URI: "b.md", Title: "B", Collection: "demo", Direction: "outbound", Resolved: true},
			{URI: "missing.md", Direction: "outbound", Resolved: false},
		},
		Inbound: []memory.RelatedNeighbor{
			{URI: "c.md", Title: "C", Collection: "demo", Direction: "inbound", Resolved: true},
		},
	}
	var buf bytes.Buffer
	renderRelated(&buf, res)
	out := buf.String()

	require.Contains(t, out, "DIRECTION")
	require.Contains(t, out, "b.md")
	require.Contains(t, out, "B")
	require.Contains(t, out, "missing.md")
	require.Contains(t, out, "-") // dash placeholder for unresolved title/collection
	require.Contains(t, out, "c.md")
}

func TestWriteRelatedRowFormatsUnresolvedAsDash(t *testing.T) {
	var buf bytes.Buffer
	writeRelatedRow(&buf, memory.RelatedNeighbor{URI: "missing.md", Direction: "outbound", Resolved: false})
	require.Equal(t, "outbound\tmissing.md\t-\t-\tno\n", buf.String())
}

func TestWriteRelatedRowFormatsResolved(t *testing.T) {
	var buf bytes.Buffer
	writeRelatedRow(&buf, memory.RelatedNeighbor{
		URI: "b.md", Title: "B", Collection: "demo", Direction: "inbound", Resolved: true,
	})
	require.Equal(t, "inbound\tb.md\tB\tdemo\tyes\n", buf.String())
}

func TestWriteJSONRelatedEmptyArraysNotNull(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeJSONRelated(&buf, memory.RelatedResult{URI: "a.md"}))

	var got struct {
		URI      string `json:"uri"`
		Outbound []any  `json:"outbound"`
		Inbound  []any  `json:"inbound"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "a.md", got.URI)
	require.NotNil(t, got.Outbound)
	require.NotNil(t, got.Inbound)
	require.Empty(t, got.Outbound)
	require.Empty(t, got.Inbound)
}

func TestWriteJSONRelatedRoundTrips(t *testing.T) {
	in := memory.RelatedResult{
		URI:      "a.md",
		Outbound: []memory.RelatedNeighbor{{URI: "b.md", Direction: "outbound", Resolved: true}},
	}
	var buf bytes.Buffer
	require.NoError(t, writeJSONRelated(&buf, in))

	var got memory.RelatedResult
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "a.md", got.URI)
	require.Equal(t, in.Outbound, got.Outbound)
	require.Empty(t, got.Inbound)
}
