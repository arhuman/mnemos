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
type noopEmbedder struct{ name string }

// New returns the no-op embedder. No model is loaded and Embed never succeeds,
// but modelDir still sets the reported Model name so both builds agree on the
// identity for a given directory.
func New(modelDir string) (Embedder, error) {
	return noopEmbedder{name: ModelName(modelDir)}, nil
}

// Embed always returns ErrNotSupported in the default build.
func (noopEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrNotSupported
}

// Dim reports the nominal dimensionality so callers can size buffers uniformly.
func (noopEmbedder) Dim() int { return Dim }

// Model reports the model name derived from the directory New was given.
func (e noopEmbedder) Model() string { return e.name }
