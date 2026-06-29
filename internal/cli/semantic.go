package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/embed"
)

// noEmbedSupportMsg is shown when a default-build binary is asked to do semantic
// work it was not compiled for.
const noEmbedSupportMsg = "built without embedding support — rebuild with -tags embed"

// loadEmbedder resolves the installed default model and constructs an Embedder.
// It is only meaningful in an embed-tagged build (embed.Supported); callers must
// gate on that first. It returns a clear, actionable error when the model has
// not been downloaded yet.
func loadEmbedder() (embed.Embedder, error) {
	dir, err := embed.ModelDir(embed.DefaultModel)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		return nil, fmt.Errorf(
			"model %q not installed at %s (run: mnemos models install %s)",
			embed.DefaultModel, dir, embed.DefaultModel,
		)
	}
	e, err := embed.New(dir)
	if err != nil {
		return nil, err
	}

	return e, nil
}
