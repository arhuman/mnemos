package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// EncodeVector serializes an embedding to a little-endian float32 BLOB (4 bytes
// per dimension). It is the inverse of decodeVector. Storing the raw float32
// bytes keeps vectors compact and lets the linear scan decode them directly.
func EncodeVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}

	return buf
}

// decodeVector parses a little-endian float32 BLOB produced by EncodeVector. It
// errors when the byte length is not a multiple of four (a corrupt vector).
func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("storage: vector blob length %d is not a multiple of 4", len(b))
	}
	vec := make([]float32, len(b)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}

	return vec, nil
}

// UpsertEmbedding inserts or replaces the vector for chunkID within tx. The
// vector is stored as the encoded BLOB; dim and model are stored alongside so a
// later model change can be detected. The reindex path batches many upserts in a
// single transaction.
func UpsertEmbedding(ctx context.Context, tx *sql.Tx, chunkID, model string, dim int, vector []byte) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO embeddings (chunk_id, model, dim, vector)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET
			model  = excluded.model,
			dim    = excluded.dim,
			vector = excluded.vector
	`, chunkID, model, dim, vector)
	if err != nil {
		return fmt.Errorf("storage: upsert embedding %q: %w", chunkID, err)
	}

	return nil
}

// getEmbedding returns the stored model, dim and decoded vector for chunkID, or
// found=false when no embedding exists. It is used by tests and diagnostics; the
// vector search reads vectors in bulk via the join in the search package.
func getEmbedding(ctx context.Context, db *sql.DB, chunkID string) (model string, dim int, vector []float32, found bool, err error) {
	var blob []byte
	row := db.QueryRowContext(ctx, `SELECT model, dim, vector FROM embeddings WHERE chunk_id = ?`, chunkID)
	switch err = row.Scan(&model, &dim, &blob); {
	case errors.Is(err, sql.ErrNoRows):
		return "", 0, nil, false, nil
	case err != nil:
		return "", 0, nil, false, fmt.Errorf("storage: get embedding %q: %w", chunkID, err)
	}
	vector, err = decodeVector(blob)
	if err != nil {
		return "", 0, nil, false, err
	}

	return model, dim, vector, true, nil
}

// deleteEmbedding removes the embedding for chunkID. Deleting a chunk_id with no
// embedding is a no-op. Embeddings are normally evicted automatically by the
// ON DELETE CASCADE when their chunk is deleted (re-ingest/forget); this is the
// explicit accessor for targeted removal.
func deleteEmbedding(ctx context.Context, db *sql.DB, chunkID string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM embeddings WHERE chunk_id = ?`, chunkID); err != nil {
		return fmt.Errorf("storage: delete embedding %q: %w", chunkID, err)
	}

	return nil
}

// CountEmbeddings returns the number of stored vectors for model. It backs the
// reindex summary and status reporting, and lets cross-package tests assert that
// the reindex path actually persisted vectors.
func CountEmbeddings(ctx context.Context, db *sql.DB, model string) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings WHERE model = ?`, model).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count embeddings for %q: %w", model, err)
	}

	return n, nil
}

// ChunkRef is a minimal chunk projection (id + content) used by the reindex path
// to embed every stored chunk without loading the full chunk rows.
type ChunkRef struct {
	ID      string
	Content string
}

// AllChunkRefs returns the id and content of every chunk, ordered by id for
// deterministic batching. The reindex path embeds these and stores one vector
// per chunk. The count is bounded by the corpus size (tens of thousands for the
// personal-use target), so materializing id+content is acceptable.
func AllChunkRefs(ctx context.Context, db *sql.DB) ([]ChunkRef, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, content FROM chunks ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list chunk refs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var refs []ChunkRef
	for rows.Next() {
		var r ChunkRef
		if err := rows.Scan(&r.ID, &r.Content); err != nil {
			return nil, fmt.Errorf("storage: scan chunk ref: %w", err)
		}
		refs = append(refs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate chunk refs: %w", err)
	}

	return refs, nil
}
