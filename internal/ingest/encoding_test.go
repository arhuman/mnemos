package ingest_test

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
	"github.com/arhuman/mnemos/internal/ingest"
)

// writeBytes creates a file with raw (non-UTF-8) bytes, which the string-based
// write helper cannot express.
func writeBytes(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, content, 0o644))
}

// cp1250Unit is a Delphi unit skeleton with a Windows-1250 comment: 0xE2 is "â"
// and 0xBA/0xFE are the S/T-with-comma letters. It holds no NUL byte and is not
// valid UTF-8 (0xE2 is a lone lead byte), which is exactly the shape issue #35
// reports being skipped as binary.
var cp1250Unit = []byte("unit Calc;\ninterface\n// C\xE2mpul de intrare\n// \xBAi \xFE\xE3rile\nimplementation\nend.\n")

func encodingOpts(root string) ingest.Options {
	return ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.pas", "**/*.dfm"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Without a declared charset, legacy-encoded text is still skipped: guessing is
// what the feature deliberately avoids.
func TestPipelineSkipsLegacyEncodingByDefault(t *testing.T) {
	src := t.TempDir()
	writeBytes(t, src, "cp1250.pas", cp1250Unit)

	p := ingest.New(newDB(t), discardLogger())
	sum, err := p.Run(context.Background(), encodingOpts(src))
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesIngested)
	require.Equal(t, 1, sum.FilesSkipped)
}

// The issue's reproduction: with a rule declaring the charset, the legacy file
// ingests alongside its ASCII and UTF-8 peers, while NUL-bearing content stays
// rejected.
func TestPipelineDecodesDeclaredEncoding(t *testing.T) {
	src := t.TempDir()
	write(t, src, "ascii.pas", "unit A;\n// Simple calculator unit\nend.\n")
	write(t, src, "utf8.pas", "unit U;\n// Câmpul de intrare\nend.\n")
	writeBytes(t, src, "cp1250.pas", cp1250Unit)
	writeBytes(t, src, "binary.dfm", []byte("object Form1\x00\x00BINARY\x00PAYLOAD\nend\n"))

	db := newDB(t)
	p := ingest.New(db, discardLogger(),
		ingest.WithEncodings([]ingest.EncodingRule{
			{Match: []string{"**/*.pas"}, Charset: "windows-1250"},
		}))

	sum, err := p.Run(context.Background(), encodingOpts(src))
	require.NoError(t, err)
	require.Equal(t, 4, sum.FilesScanned)
	require.Equal(t, 3, sum.FilesIngested, "ascii, utf8 and cp1250 all ingest")
	require.Equal(t, 1, sum.FilesSkipped, "only the NUL-bearing file is skipped")

	// Counting files would pass even on a wrong decode; assert the stored text is
	// the intended Romanian, so mojibake is a failure rather than a silent pass.
	// 0xBA/0xFE decode to ş/ţ (cedilla, U+015F/U+0163), which is what cp1250
	// actually maps them to; the comma-below letters are a different codepoint.
	require.Contains(t, storedText(t, db, "cp1250.pas"), "Câmpul de intrare")
	require.Contains(t, storedText(t, db, "cp1250.pas"), "şi ţările")
}

// storedText returns the concatenated chunk text indexed for uri.
func storedText(t *testing.T, db *sql.DB, uri string) string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT c.content FROM chunks c JOIN documents d ON d.id = c.document_id
		 WHERE d.uri = ? ORDER BY c.ordinal`, uri)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var sb strings.Builder
	for rows.Next() {
		var text string
		require.NoError(t, rows.Scan(&text))
		_, _ = sb.WriteString(text)
	}
	require.NoError(t, rows.Err())

	return sb.String()
}

// A declared charset must never override the NUL check, or a rule broad enough
// to cover binary siblings would admit binary content.
func TestPipelineEncodingRuleNeverAdmitsNUL(t *testing.T) {
	src := t.TempDir()
	writeBytes(t, src, "binary.dfm", []byte("object Form1\x00\x00TPF0\x00payload\n"))

	p := ingest.New(newDB(t), discardLogger(),
		ingest.WithEncodings([]ingest.EncodingRule{
			{Match: []string{"**/*"}, Charset: "windows-1250"},
		}))

	sum, err := p.Run(context.Background(), encodingOpts(src))
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesIngested)
	require.Equal(t, 1, sum.FilesSkipped)
}

// Decoded content is hashed, so correcting a charset re-ingests the file instead
// of letting the previous mis-decode survive the unchanged-hash skip.
func TestPipelineEncodingChangeReingests(t *testing.T) {
	src := t.TempDir()
	writeBytes(t, src, "legacy.pas", cp1250Unit)
	db := newDB(t)
	opts := encodingOpts(src)

	wrong := ingest.New(db, discardLogger(), ingest.WithEncodings([]ingest.EncodingRule{
		{Match: []string{"**/*.pas"}, Charset: "windows-1252"},
	}))
	sum, err := wrong.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 1, sum.FilesIngested)

	// Same bytes on disk, different declared charset: the decoded content differs,
	// so the hash differs and the file is re-ingested rather than hash-skipped.
	right := ingest.New(db, discardLogger(), ingest.WithEncodings([]ingest.EncodingRule{
		{Match: []string{"**/*.pas"}, Charset: "windows-1250"},
	}))
	sum2, err := right.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 1, sum2.FilesIngested, "charset correction must not be hash-skipped")
	require.Equal(t, 0, sum2.FilesSkipped)
}

// The first matching rule wins, so a narrower rule listed first overrides a
// broad one.
func TestPipelineEncodingFirstMatchWins(t *testing.T) {
	src := t.TempDir()
	writeBytes(t, src, "legacy/unit.pas", cp1250Unit)

	p := ingest.New(newDB(t), discardLogger(),
		ingest.WithEncodings([]ingest.EncodingRule{
			{Match: []string{"legacy/**"}, Charset: "windows-1250"},
			{Match: []string{"**/*.pas"}, Charset: "utf-8"},
		}))

	sum, err := p.Run(context.Background(), encodingOpts(src))
	require.NoError(t, err)
	require.Equal(t, 1, sum.FilesIngested, "the narrower cp1250 rule applies, not the utf-8 catch-all")
}

// A charset that cannot be resolved must surface as an error, never degrade to
// UTF-8-only: that would silently skip the files the rule was added to ingest.
func TestPipelineUnknownCharsetIsAnError(t *testing.T) {
	src := t.TempDir()
	writeBytes(t, src, "legacy.pas", cp1250Unit)

	p := ingest.New(newDB(t), discardLogger(),
		ingest.WithEncodings([]ingest.EncodingRule{
			{Match: []string{"**/*.pas"}, Charset: "definitely-not-a-charset"},
		}))

	_, err := p.Run(context.Background(), encodingOpts(src))
	require.Error(t, err)
	require.Contains(t, err.Error(), "definitely-not-a-charset")
}
