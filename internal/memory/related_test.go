package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/testutil"
)

// seedLinkGraph seeds a small vault under docs/: a.md links to b.md and a
// dangling ghost.md; b.md links back to a.md. Relative link targets resolve
// against the source file's directory, so the links land under docs/.
func seedLinkGraph(t *testing.T, f fixture) {
	t.Helper()
	f.seed(t, "docs/a.md", "# A\n\nSee [B](b.md) and [Ghost](ghost.md).\n", "default")
	f.seed(t, "docs/b.md", "# B\n\nBack to [A](a.md).\n", "default")
}

func TestRelatedBothDirections(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)

	res, err := f.svc.Related(context.Background(), "docs/a.md", memory.DirectionBoth, 0)
	require.NoError(t, err)
	require.Equal(t, "docs/a.md", res.URI)

	// Outbound: docs/b.md (resolved) and docs/ghost.md (dangling), ordered by uri.
	require.Len(t, res.Outbound, 2)
	require.Equal(t, "docs/b.md", res.Outbound[0].URI)
	require.True(t, res.Outbound[0].Resolved)
	require.Equal(t, "docs/ghost.md", res.Outbound[1].URI)
	require.False(t, res.Outbound[1].Resolved)

	// Inbound: docs/b.md links back to docs/a.md.
	require.Len(t, res.Inbound, 1)
	require.Equal(t, "docs/b.md", res.Inbound[0].URI)
	require.True(t, res.Inbound[0].Resolved)
}

func TestRelatedDirectionFilter(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)
	ctx := context.Background()

	out, err := f.svc.Related(ctx, "docs/a.md", memory.DirectionOutbound, 0)
	require.NoError(t, err)
	require.NotEmpty(t, out.Outbound)
	require.Nil(t, out.Inbound)

	in, err := f.svc.Related(ctx, "docs/a.md", memory.DirectionInbound, 0)
	require.NoError(t, err)
	require.Nil(t, in.Outbound)
	require.NotEmpty(t, in.Inbound)
}

func TestRelatedUnknownURIIsNotFound(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)

	_, err := f.svc.Related(context.Background(), "docs/missing.md", memory.DirectionBoth, 0)
	require.Error(t, err)

	_, err = f.svc.Related(context.Background(), "", memory.DirectionBoth, 0)
	require.Error(t, err)
}

func TestRelatedHidesDeniedCollections(t *testing.T) {
	f := newFixture(t, false, false)
	seedLinkGraph(t, f)
	// A secret doc that links to docs/a.md; its backlink must not surface once the
	// collection is on the visibility deny list.
	f.seed(t, "docs/secret.md", "# Secret\n\nRefers to [A](a.md).\n", "secret")

	// Without a deny list, the secret backlink is visible.
	visible, err := f.svc.Related(context.Background(), "docs/a.md", memory.DirectionInbound, 0)
	require.NoError(t, err)
	require.Len(t, visible.Inbound, 2)

	// With "secret" denied, only docs/b.md remains.
	cfgDeny, err := config.Load("", func(string) bool { return false })
	require.NoError(t, err)
	cfgDeny.Security.Visibility.Deny = []string{"secret"}
	denySvc := memory.New(f.db, cfgDeny, f.treeRoot, nil, testutil.DiscardLogger())

	hidden, err := denySvc.Related(context.Background(), "docs/a.md", memory.DirectionInbound, 0)
	require.NoError(t, err)
	require.Len(t, hidden.Inbound, 1)
	require.Equal(t, "docs/b.md", hidden.Inbound[0].URI)
}

func TestRelatedLimitPerDirection(t *testing.T) {
	f := newFixture(t, false, false)
	// hub.md links to three targets; limit=2 caps outbound at two.
	f.seed(t, "docs/hub.md", "# Hub\n\n[1](one.md) [2](two.md) [3](three.md)\n", "default")

	res, err := f.svc.Related(context.Background(), "docs/hub.md", memory.DirectionOutbound, 2)
	require.NoError(t, err)
	require.Len(t, res.Outbound, 2)
}
