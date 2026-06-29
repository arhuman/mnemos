package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/embed"
)

// maxModelBytes caps a single downloaded file so a misbehaving or hostile
// endpoint cannot exhaust the disk. The largest expected weight (all-MiniLM-L6
// fp32 ONNX) is well under this 2 GiB ceiling.
const maxModelBytes = 2 << 30

// modelFile pairs a remote download URL with the local filename it is saved as.
type modelFile struct {
	url  string
	name string
}

// modelDownloads maps a supported model name to the files `models install`
// fetches. The all-MiniLM-L6-v2 fp32 ONNX export and tokenizer come from the
// sentence-transformers Hugging Face repo (resolve URLs serve the raw bytes).
var modelDownloads = map[string][]modelFile{
	embed.DefaultModel: {
		{
			url:  "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/onnx/model.onnx",
			name: "model.onnx",
		},
		{
			url:  "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/resolve/main/tokenizer.json",
			name: "tokenizer.json",
		},
	},
}

// newModelsCmd builds the `models` command group. Downloading is independent of
// the embed build tag (it only writes files), so it compiles and runs in the
// default build; the downloaded weights are used by an embed-tagged binary.
func newModelsCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage local embedding models",
	}
	cmd.AddCommand(newModelsInstallCmd(state))

	return cmd
}

func newModelsInstallCmd(_ *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "install <model>",
		Short: "Download an embedding model into ~/.mnemos/models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsInstall(cmd, args[0])
		},
	}
}

func runModelsInstall(cmd *cobra.Command, model string) error {
	files, ok := modelDownloads[model]
	if !ok {
		return fmt.Errorf("models: unknown model %q (supported: %s)", model, embed.DefaultModel)
	}

	dir, err := embed.ModelDir(model)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("models: create %q: %w", dir, err)
	}

	client := &http.Client{Timeout: 15 * time.Minute}
	out := cmd.OutOrStdout()
	for _, f := range files {
		dest := filepath.Join(dir, f.name)
		_, _ = fmt.Fprintf(out, "downloading %s -> %s\n", f.name, dest)
		if err := downloadFile(cmd.Context(), client, f.url, dest); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(out, "installed %s into %s\n", model, dir)

	return nil
}

// downloadFile streams url to dest via a temporary file, renaming on success so
// a partial download never masquerades as a complete model file.
func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("models: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}

		return fmt.Errorf("models: download %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models: download %s: unexpected status %s", url, resp.Status)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp) //nolint:gosec // tmp path derived from the tool-controlled model destination dir
	if err != nil {
		return fmt.Errorf("models: create %q: %w", tmp, err)
	}
	// Cap the stream so a misbehaving or hostile endpoint cannot fill the disk;
	// read one byte past the ceiling to detect an over-limit body.
	n, err := io.Copy(f, &io.LimitedReader{R: resp.Body, N: maxModelBytes + 1})
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("models: write %q: %w", tmp, err)
	}
	if n > maxModelBytes {
		_ = f.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("models: download %s exceeds the %d-byte limit", url, maxModelBytes)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("models: close %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("models: finalize %q: %w", dest, err)
	}

	return nil
}
