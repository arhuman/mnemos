package ingest

import (
	"strconv"

	"github.com/cespare/xxhash/v2"
)

// documentID returns a deterministic, content-addressed id for a document
// keyed by its collection and uri. Using collection + "\x00" + uri means the
// same path in two collections gets distinct ids while re-ingesting the same
// path is idempotent.
func documentID(collection, uri string) string {
	h := xxhash.New()
	_, _ = h.WriteString(collection)
	_, _ = h.Write([]byte{0})
	_, _ = h.WriteString(uri)

	return hex64(h.Sum64())
}

// chunkID returns a deterministic id for the ordinal-th chunk of a document.
func chunkID(docID string, ordinal int) string {
	h := xxhash.New()
	_, _ = h.WriteString(docID)
	_, _ = h.WriteString(":")
	_, _ = h.WriteString(strconv.Itoa(ordinal))

	return hex64(h.Sum64())
}

// hex64 renders a 64-bit hash as a 16-char hex string.
func hex64(v uint64) string {
	return strconv.FormatUint(v, 16)
}
