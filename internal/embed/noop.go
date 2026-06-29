//go:build !embed

package embed

import "context"

// Supported reports whether this binary was built with semantic-embedding
// support. The default (no-tag) build is false; rebuild with -tags embed to
// enable the ONNX embedder. CLI and retrieval code branch on this constant so
// they compile identically in both builds.
const Supported = false

// noopEmbedder is the default-build Embedder. It performs no inference: Embed
// always fails with ErrNotSupported so retrieval cleanly degrades to lexical
// search and the CLI can print a "rebuild with -tags embed" message.
type noopEmbedder struct{}

// New returns the no-op embedder. modelDir is ignored in the default build; the
// returned embedder never loads a model and never succeeds at embedding.
func New(_ string) (Embedder, error) {
	return noopEmbedder{}, nil
}

// Embed always returns ErrNotSupported in the default build.
func (noopEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrNotSupported
}

// Dim reports the nominal dimensionality so callers can size buffers uniformly.
func (noopEmbedder) Dim() int { return Dim }

// Model reports the nominal model name.
func (noopEmbedder) Model() string { return DefaultModel }
