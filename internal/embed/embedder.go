// Package embed turns text into dense sentence embeddings for semantic search.
//
// The heavy ONNX inference stack (gomlx + onnx-gomlx + sugarme/tokenizer) lives
// behind the "embed" build tag in onnx.go, so the default build stays lean and
// cgo-free with only the no-op embedder (noop.go) compiled in. Callers depend on
// the Embedder interface and the build-tag-selected New constructor; the rest of
// the package (pooling, normalization, the model/dim constants) is pure Go and
// shared by both builds so it can be unit-tested without the tag or a model.
package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultModel is the model used when [embedding].model is unset: the
// sentence-transformers all-MiniLM-L6-v2 checkpoint. It is the fallback name,
// not the name every vector carries: an embedder reports the model it actually
// loaded (see ModelName).
const DefaultModel = "all-MiniLM-L6-v2"

// Dim is the embedding dimensionality of all-MiniLM-L6-v2.
const Dim = 384

// ErrNotSupported is returned by the no-op embedder (default build). Retrieval
// code treats it as "semantic search unavailable" and degrades to lexical-only
// rather than failing. Inspect it with errors.Is.
var ErrNotSupported = errors.New("embed: built without embedding support (rebuild with -tags embed)")

// Embedder turns texts into L2-normalized sentence embeddings. Implementations
// must return one vector per input text, each of length Dim. Embed is the single
// blocking method and takes a context so long batches can be cancelled.
type Embedder interface {
	// Embed returns one L2-normalized Dim-length vector per input text. An empty
	// input yields a nil slice and no error.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dim reports the embedding dimensionality.
	Dim() int
	// Model reports the model name persisted with each vector.
	Model() string
}

// ModelDir returns the on-disk directory for a model under the user's mnemos
// home (~/.mnemos/models/<model>). It is where `models install` downloads the
// weights and where New loads them from. It does not create the directory.
func ModelDir(model string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("embed: resolve home dir: %w", err)
	}

	return filepath.Join(home, ".mnemos", "models", model), nil
}

// ModelName is the inverse of ModelDir: it recovers the model name from a model
// directory, and is what an embedder reports as the identity of the weights it
// loaded. A trailing separator is tolerated. An empty or filesystem-root dir
// yields DefaultModel, so a caller that never set a name still records the
// conventional one rather than an empty or nonsense label.
func ModelName(modelDir string) string {
	base := filepath.Base(filepath.Clean(modelDir))
	switch base {
	case ".", string(filepath.Separator), "":
		return DefaultModel
	}

	return base
}

// ResolveModel picks the embedding model name: the configured value when set,
// otherwise DefaultModel. Callers pass Config.Embedding.Model straight in, so
// the empty-means-default rule lives in one place.
func ResolveModel(configured string) string {
	if configured == "" {
		return DefaultModel
	}

	return configured
}
