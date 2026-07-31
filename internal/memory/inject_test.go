package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
)

// neighborURIs collapses a neighbor slice to a uri set for assertions.
func neighborURIs(ns []memory.RelatedNeighbor) map[string]bool {
	out := make(map[string]bool, len(ns))
	for _, n := range ns {
		out[n.URI] = true
	}

	return out
}

func TestReadFollowLinksOptIn(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f) // docs/a.md -> {b.md, ghost.md}; docs/b.md -> a.md
	ctx := context.Background()

	// Default read attaches nothing.
	plain, err := f.svc.ReadOneOpts(ctx, "docs/a.md", "", memory.ReadOptions{})
	require.NoError(t, err)
	require.Empty(t, plain.Neighbors)

	// follow_links attaches outbound (resolved b.md + dangling ghost.md) and
	// inbound (b.md backlink).
	res, err := f.svc.ReadOneOpts(ctx, "docs/a.md", "", memory.ReadOptions{FollowLinks: true})
	require.NoError(t, err)
	uris := neighborURIs(res.Neighbors)
	require.True(t, uris["docs/b.md"], "outbound + inbound b.md")
	require.True(t, uris["docs/ghost.md"], "dangling outbound ghost.md")
}

func TestReadFollowLinksDirectionAndLimit(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)
	ctx := context.Background()

	// Inbound only: docs/a.md's backlink is docs/b.md; ghost.md (outbound) absent.
	in, err := f.svc.ReadOneOpts(ctx, "docs/a.md", "", memory.ReadOptions{
		FollowLinks:   true,
		LinkDirection: memory.DirectionInbound,
	})
	require.NoError(t, err)
	uris := neighborURIs(in.Neighbors)
	require.True(t, uris["docs/b.md"])
	require.False(t, uris["docs/ghost.md"])
	for _, n := range in.Neighbors {
		require.Equal(t, "inbound", n.Direction)
	}
}

func TestContextFollowLinksOptIn(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)
	ctx := context.Background()

	// "Ghost" appears only in docs/a.md, so it is the top block.
	q := search.Query{Text: "Ghost"}

	plain, err := f.svc.ContextWithOptions(ctx, f.retriever, q, memory.ContextOptions{GroupByDocument: true})
	require.NoError(t, err)
	require.NotEmpty(t, plain)
	require.Empty(t, plain[0].Neighbors)

	linked, err := f.svc.ContextWithOptions(ctx, f.retriever, q, memory.ContextOptions{
		GroupByDocument: true,
		FollowLinks:     true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, linked)
	require.Contains(t, linked[0].Source, "docs/a.md:")
	require.True(t, neighborURIs(linked[0].Neighbors)["docs/b.md"], "a.md's neighbors should include b.md")
}
