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

// loadEmbedder resolves the configured model ([embedding].model, empty meaning
// embed.DefaultModel) and constructs an Embedder over its directory. It is only
// meaningful in an embed-tagged build (embed.Supported); callers must gate on
// that first. It returns a clear, actionable error when the model has not been
// downloaded yet.
func loadEmbedder(model string) (embed.Embedder, error) {
	model = embed.ResolveModel(model)
	dir, err := embed.ModelDir(model)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		return nil, fmt.Errorf(
			"model %q not installed at %s (run: mnemos models install %s)",
			model, dir, model,
		)
	}
	e, err := embed.New(dir)
	if err != nil {
		return nil, err
	}

	return e, nil
}
