package ingest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/security"
	"github.com/arhuman/mnemos/internal/storage"
)

func okfyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	return db
}

func baseOkfyOptions(root, source string) OkfyOptions {
	return OkfyOptions{
		TreeRoot: root,
		Source:   source,
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
		Scanner:  security.NewRegexScanner(),
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestOkfyTxtPrependsHeadingAndIndexes(t *testing.T) {
	db := okfyTestDB(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.txt"), []byte("Plain body without a heading.\n"), 0o644))

	res, err := Okfy(context.Background(), db, discard(), baseOkfyOptions(root, "note.txt"))
	require.NoError(t, err)
	require.Equal(t, "note.md", res.URI)
	require.Equal(t, "note.txt", res.SourceURI)
	require.Equal(t, "default", res.Collection)
	require.NotEmpty(t, res.DocumentID)
	require.Positive(t, res.Chunks)

	// Source kept; OKF output carries frontmatter, default type, and a derived heading.
	require.FileExists(t, filepath.Join(root, "note.txt"))
	out, err := os.ReadFile(filepath.Join(root, "note.md"))
	require.NoError(t, err)
	body := string(out)
	require.True(t, strings.HasPrefix(body, "---\n"))
	require.Contains(t, body, "type: document")
	require.Contains(t, body, "# note")
}

func TestOkfyHonorsOutTypeTagsCollection(t *testing.T) {
	db := okfyTestDB(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("# Already titled\n\nBody.\n"), 0o644))

	opts := baseOkfyOptions(root, "src.txt")
	opts.Out = "docs/result.md"
	opts.Collection = "tech"
	opts.Type = "idea"
	opts.Tags = []string{"a", "b"}

	res, err := Okfy(context.Background(), db, discard(), opts)
	require.NoError(t, err)
	require.Equal(t, "docs/result.md", res.URI)
	require.Equal(t, "tech", res.Collection)

	out, err := os.ReadFile(filepath.Join(root, "docs", "result.md"))
	require.NoError(t, err)
	body := string(out)
	require.Contains(t, body, "type: idea")
	require.Contains(t, body, "collection: tech")
	require.Contains(t, body, "- a")
	require.Contains(t, body, "- b")
	// Source already had a heading: no derived "# src" title was prepended.
	require.NotContains(t, body, "# src\n")
}

func TestOkfyRejects(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.txt"), []byte("Body.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "doc.md"), []byte("# Doc\n\nBody.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "leak.txt"), []byte("AKIAQYLPMN5HXYZ12345 secret\n"), 0o644))

	cases := []struct {
		name    string
		mutate  func(*OkfyOptions)
		wantSub string
	}{
		{"traversal out", func(o *OkfyOptions) { o.Source = "source.txt"; o.Out = "../escape.md" }, "okfy: destination:"},
		{"non text source", func(o *OkfyOptions) { o.Source = "data.json" }, "must be a .txt or .md file"},
		{"md equals source", func(o *OkfyOptions) { o.Source = "doc.md" }, "equals source"},
		{"out not md", func(o *OkfyOptions) { o.Source = "source.txt"; o.Out = "out.txt" }, "must have a .md extension"},
		{"secret in source", func(o *OkfyOptions) { o.Source = "leak.txt" }, "detected secrets"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := okfyTestDB(t)
			opts := baseOkfyOptions(root, "source.txt")
			tc.mutate(&opts)
			_, err := Okfy(context.Background(), db, discard(), opts)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestOkfyExistingOutputRequiresForce(t *testing.T) {
	db := okfyTestDB(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.txt"), []byte("Body.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "taken.md"), []byte("existing\n"), 0o644))

	opts := baseOkfyOptions(root, "source.txt")
	opts.Out = "taken.md"
	_, err := Okfy(context.Background(), db, discard(), opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	opts.Force = true
	res, err := Okfy(context.Background(), db, discard(), opts)
	require.NoError(t, err)
	require.Equal(t, "taken.md", res.URI)
}

func TestOkfyWritesLogEntryForCreateAndUpdate(t *testing.T) {
	db := okfyTestDB(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "src.txt"), []byte("Body.\n"), 0o644))

	opts := baseOkfyOptions(root, "src.txt")
	opts.Out = "result.md"

	// First conversion: a Creation entry is logged next to the output.
	_, err := Okfy(context.Background(), db, discard(), opts)
	require.NoError(t, err)

	logPath := filepath.Join(root, "log.md")
	logBody, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(logBody), "- **Creation** [result](./result.md)")
	require.False(t, strings.HasPrefix(string(logBody), "---"), "log.md must have no frontmatter")

	// Overwriting with --force logs an Update entry above the Creation entry.
	opts.Force = true
	_, err = Okfy(context.Background(), db, discard(), opts)
	require.NoError(t, err)

	logBody, err = os.ReadFile(logPath)
	require.NoError(t, err)
	body := string(logBody)
	require.Contains(t, body, "- **Update** [result](./result.md)")
	require.Less(t, strings.Index(body, "**Update**"), strings.Index(body, "**Creation**"),
		"newest (Update) entry first")
}

func TestHasMarkdownHeading(t *testing.T) {
	require.True(t, hasMarkdownHeading("# Title\n\nbody"))
	require.True(t, hasMarkdownHeading("intro\n## Section"))
	require.False(t, hasMarkdownHeading("no heading here\nplain"))
	require.False(t, hasMarkdownHeading("#nospace is not a heading"))
}

func TestFindingRulesDedupesAndSorts(t *testing.T) {
	got := findingRules([]security.Finding{
		{Rule: "github-token"},
		{Rule: "aws-access-key-id"},
		{Rule: "github-token"},
	})
	require.Equal(t, []string{"aws-access-key-id", "github-token"}, got)
}
