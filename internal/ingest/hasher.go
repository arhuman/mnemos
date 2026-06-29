package ingest

import "github.com/cespare/xxhash/v2"

// hashContent returns the hex xxhash of the file bytes, used as
// documents.content_hash for change detection. xxhash is fast and non-crypto;
// collision risk is irrelevant for "did this file change?" semantics.
func hashContent(content []byte) string {
	return hex64(xxhash.Sum64(content))
}
