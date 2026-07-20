package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// seedTitled writes a markdown doc carrying a frontmatter title so tests can
// assert the true documents.title is returned rather than a heading-derived one.
const scimTitled = `---
title: SCIM Provisioning Guide
---

# Provisioning

SCIM provisioning syncs users automatically from the IdP.
`

func TestSearchReturnsTrueTitle(t *testing.T) {
	f := newFixture(t, false, false)
	f.seed(t, "docs/scim.md", scimTitled, "default")

	results, err := f.svc.Search(context.Background(), f.retriever, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	// The result carries the real documents.title, not a heading-derived label:
	// it equals exactly what storage holds for the owning document.
	doc, err := storage.GetDocumentByURI(context.Background(), f.db, results[0].URI)
	require.NoError(t, err)
	require.NotEmpty(t, doc.Title)
	require.Equal(t, doc.Title, results[0].Title)
}

func TestReadDocumentSectionAndLinesScope(t *testing.T) {
	f := newFixture(t, false, false)
	uri := f.seed(t, "docs/scim.md", scimTitled, "default")
	ctx := context.Background()

	// A section that matches keeps the doc; one that matches nothing errors rather
	// than returning an empty read.
	got, err := f.svc.ReadDocumentOpts(ctx, uri, memory.ReadOptions{Section: "provisioning"})
	require.NoError(t, err)
	require.Contains(t, got.Content, "SCIM provisioning")

	_, err = f.svc.ReadDocumentOpts(ctx, uri, memory.ReadOptions{Section: "no-such-heading"})
	require.Error(t, err)

	// A line span overlapping the body keeps it; one far past the doc errors.
	// (Lines 1-3 are frontmatter; the body chunk starts at line 4.)
	got, err = f.svc.ReadDocumentOpts(ctx, uri, memory.ReadOptions{Lines: "5-6"})
	require.NoError(t, err)
	require.NotEmpty(t, got.Content)

	_, err = f.svc.ReadDocumentOpts(ctx, uri, memory.ReadOptions{Lines: "9000-9001"})
	require.Error(t, err)

	// A malformed range is a caller error, not a whole-document read.
	_, err = f.svc.ReadDocumentOpts(ctx, uri, memory.ReadOptions{Lines: "bad"})
	require.Error(t, err)
}

func TestReadBudgetTruncates(t *testing.T) {
	f := newFixture(t, false, false)
	uri := f.seed(t, "docs/scim.md", scimTitled, "default")
	ctx := context.Background()

	full, err := f.svc.ReadOne(ctx, uri, "")
	require.NoError(t, err)
	require.False(t, full.Truncated)

	clipped, err := f.svc.ReadOneOpts(ctx, uri, "", memory.ReadOptions{MaxChars: 10})
	require.NoError(t, err)
	require.True(t, clipped.Truncated)
	require.Less(t, len(clipped.Content), len(full.Content))
}

func TestContextGroupingAndBudget(t *testing.T) {
	f := newFixture(t, false, false)
	f.seed(t, "docs/a.md", "# Alpha\n\nprovisioning alpha content here.\n", "default")
	f.seed(t, "docs/b.md", "# Beta\n\nprovisioning beta content here.\n", "default")
	ctx := context.Background()
	q := search.Query{Text: "provisioning", Limit: 10}

	grouped, err := f.svc.ContextWithOptions(ctx, f.retriever, q, memory.ContextOptions{GroupByDocument: true})
	require.NoError(t, err)
	require.NotEmpty(t, grouped)
	seen := make(map[string]bool)
	for _, b := range grouped {
		uri, _, ok := strings.Cut(b.Source, ":")
		require.True(t, ok)
		require.False(t, seen[uri], "document %q grouped more than once", uri)
		seen[uri] = true
	}

	// A tight char budget clips block content and flags it.
	budgeted, err := f.svc.ContextWithOptions(ctx, f.retriever, q, memory.ContextOptions{MaxChars: 5})
	require.NoError(t, err)
	require.NotEmpty(t, budgeted)
	require.True(t, budgeted[0].Truncated)
}
