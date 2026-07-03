package doctor_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/doctor"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// seed upserts a document with its chunks and links in one committed transaction.
func seed(t *testing.T, db *sql.DB, doc model.Document, chunks []model.Chunk, links []model.Link) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
	require.NoError(t, storage.ReplaceChunks(context.Background(), tx, doc.ID, chunks))
	require.NoError(t, storage.ReplaceLinks(context.Background(), tx, doc.ID, links))
	require.NoError(t, tx.Commit())
}

// bodyChunk is a single non-empty chunk so a seeded document is not flagged as
// empty-bodied.
func bodyChunk(docID string) []model.Chunk {
	return []model.Chunk{{ID: docID + "-c0", DocumentID: docID, Ordinal: 0, Content: "body", StartLine: 1, EndLine: 1}}
}

func doc(id, uri, hash, fm string, size int64) model.Document {
	return model.Document{
		ID: id, URI: uri, Collection: "c", ContentHash: hash,
		SizeBytes: size, FrontmatterJSON: fm, IndexedAt: "2024-01-01T00:00:00Z",
	}
}

// seedFixture builds a tree exercising every detector and returns the store.
func seedFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := testutil.NewDB(t)
	ref := `{"type":"Reference"}`

	// Exact duplicates: same content hash "H".
	seed(t, db, doc("dupA", "notes/a.md", "H", ref, 10), bodyChunk("dupA"), nil)
	seed(t, db, doc("dupB", "notes/b.md", "H", ref, 10), bodyChunk("dupB"), nil)
	// Broken link: notes/c.md -> notes/ghost.md (not a document).
	seed(t, db, doc("linkSrc", "notes/c.md", "H2", ref, 10), bodyChunk("linkSrc"),
		[]model.Link{{SrcDoc: "linkSrc", DstDoc: "notes/ghost.md"}})
	// Oversized.
	seed(t, db, doc("big", "notes/big.md", "H3", ref, 60000), bodyChunk("big"), nil)
	// Tag variants (Backend/backend) + single-use (uniquetag).
	seed(t, db, doc("t1", "notes/t1.md", "H4", `{"type":"Reference","tags":["Backend","uniquetag"]}`, 10), bodyChunk("t1"), nil)
	seed(t, db, doc("t2", "notes/t2.md", "H5", `{"type":"Reference","tags":["backend"]}`, 10), bodyChunk("t2"), nil)
	// Missing frontmatter (non-reserved .md).
	seed(t, db, doc("noFm", "notes/nofm.md", "H6", "", 10), bodyChunk("noFm"), nil)
	// Reserved index.md with no frontmatter: NOT flagged, and satisfies notes/'s index.
	seed(t, db, doc("idx", "notes/index.md", "H7", "", 10), bodyChunk("idx"), nil)
	// Empty body: no chunks.
	seed(t, db, doc("empty", "notes/empty.md", "H8", ref, 10), nil, nil)
	// A directory with a document but no index.md -> missing-index.
	seed(t, db, doc("guide", "guide/x.md", "H9", ref, 10), bodyChunk("guide"), nil)

	return db
}

func byCategory(findings []doctor.Finding, cat string) []doctor.Finding {
	var out []doctor.Finding
	for _, f := range findings {
		if f.Category == cat {
			out = append(out, f)
		}
	}

	return out
}

func TestRunDetectors(t *testing.T) {
	db := seedFixture(t)
	findings, err := doctor.Run(context.Background(), db, doctor.Options{})
	require.NoError(t, err)

	t.Run("exact duplicates", func(t *testing.T) {
		dups := byCategory(findings, doctor.CategoryDuplicate)
		require.Len(t, dups, 1)
		require.Equal(t, []string{"notes/a.md", "notes/b.md"}, dups[0].URIs)
		require.Equal(t, doctor.SeverityWarn, dups[0].Severity)
	})

	t.Run("broken link", func(t *testing.T) {
		broken := byCategory(findings, doctor.CategoryBrokenLink)
		require.Len(t, broken, 1)
		require.Equal(t, []string{"notes/c.md"}, broken[0].URIs)
		require.Contains(t, broken[0].Title, "notes/ghost.md")
	})

	t.Run("oversized", func(t *testing.T) {
		over := byCategory(findings, doctor.CategoryOversized)
		require.Len(t, over, 1)
		require.Equal(t, []string{"notes/big.md"}, over[0].URIs)
	})

	t.Run("tag hygiene", func(t *testing.T) {
		tags := byCategory(findings, doctor.CategoryTagHygiene)
		var variant, single *doctor.Finding
		for i := range tags {
			switch tags[i].Severity {
			case doctor.SeverityWarn:
				variant = &tags[i]
			case doctor.SeverityInfo:
				single = &tags[i]
			default:
			}
		}
		require.NotNil(t, variant, "expected a tag-variant warning")
		require.ElementsMatch(t, []string{"notes/t1.md", "notes/t2.md"}, variant.URIs)
		require.NotNil(t, single, "expected a single-use tag info")
		require.Contains(t, single.Title, "uniquetag")
		require.Equal(t, []string{"notes/t1.md"}, single.URIs)
	})

	t.Run("structural gaps", func(t *testing.T) {
		st := byCategory(findings, doctor.CategoryStructure)
		var missingFm, emptyBody, missingIdx bool
		for _, f := range st {
			switch f.URIs[0] {
			case "notes/nofm.md":
				missingFm = true
			case "notes/empty.md":
				emptyBody = true
			case "guide/index.md":
				missingIdx = true
			case "notes/index.md":
				t.Fatal("reserved index.md must not be flagged for missing frontmatter")
			default:
			}
		}
		require.True(t, missingFm, "notes/nofm.md should be flagged for missing frontmatter")
		require.True(t, emptyBody, "notes/empty.md should be flagged as empty-bodied")
		require.True(t, missingIdx, "guide/ should be flagged for missing index.md")
	})
}

func TestRunScopedByPath(t *testing.T) {
	db := seedFixture(t)
	findings, err := doctor.Run(context.Background(), db, doctor.Options{PathPrefix: "guide/"})
	require.NoError(t, err)

	// Only guide/ issues survive scoping: the missing index.md, nothing from notes/.
	require.Empty(t, byCategory(findings, doctor.CategoryDuplicate))
	require.Empty(t, byCategory(findings, doctor.CategoryBrokenLink))
	require.Empty(t, byCategory(findings, doctor.CategoryOversized))
	for _, f := range findings {
		for _, u := range f.URIs {
			require.Truef(t, hasPrefix(u, "guide/"), "unexpected out-of-scope uri %q", u)
		}
	}
}

func TestRunEmptyStore(t *testing.T) {
	db := testutil.NewDB(t)
	findings, err := doctor.Run(context.Background(), db, doctor.Options{})
	require.NoError(t, err)
	require.Empty(t, findings)
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
