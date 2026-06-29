package ingest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDocumentIDDeterministic(t *testing.T) {
	a := documentID("col", "docs/a.md")
	b := documentID("col", "docs/a.md")
	require.Equal(t, a, b, "same collection+uri must be stable")
}

func TestDocumentIDCollectionScoped(t *testing.T) {
	require.NotEqual(t,
		documentID("col1", "docs/a.md"),
		documentID("col2", "docs/a.md"),
		"different collections must yield different ids",
	)
}

func TestChunkIDDeterministicAndOrdinalScoped(t *testing.T) {
	doc := documentID("col", "docs/a.md")
	require.Equal(t, chunkID(doc, 0), chunkID(doc, 0))
	require.NotEqual(t, chunkID(doc, 0), chunkID(doc, 1))
}

func TestHashContentChangesWithContent(t *testing.T) {
	require.Equal(t, hashContent([]byte("x")), hashContent([]byte("x")))
	require.NotEqual(t, hashContent([]byte("x")), hashContent([]byte("y")))
}
