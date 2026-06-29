package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/embed"
)

// TestModelsCmdInstallViaCobra exercises the full cobra wiring for
// `models install <model>`, covering the RunE closure in newModelsInstallCmd.
// It overrides the download URLs to point at a local httptest server so no
// real network traffic is made.
func TestModelsCmdInstallViaCobra(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fake-model-payload"))
	}))
	t.Cleanup(srv.Close)

	orig := modelDownloads[embed.DefaultModel]
	modelDownloads[embed.DefaultModel] = []modelFile{
		{url: srv.URL + "/model.onnx", name: "model.onnx"},
		{url: srv.URL + "/tokenizer.json", name: "tokenizer.json"},
	}
	t.Cleanup(func() { modelDownloads[embed.DefaultModel] = orig })
	t.Setenv("HOME", t.TempDir())

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"models", "install", embed.DefaultModel})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	root.SetContext(ctx)
	require.NoError(t, root.Execute())

	dir, err := embed.ModelDir(embed.DefaultModel)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "model.onnx"))
	require.FileExists(t, filepath.Join(dir, "tokenizer.json"))
	require.Contains(t, out.String(), "installed")
}

// TestModelsCmdInstallUnknownViaCobra checks that passing an unknown model name
// through the full cobra invocation returns a clear "unknown model" error.
func TestModelsCmdInstallUnknownViaCobra(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"models", "install", "not-a-model"})
	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model")
}

// TestDownloadFileSizeCapOverflow is intentionally omitted: maxModelBytes is
// 2 GiB and triggering the overflow branch would require streaming 2 GiB+1
// bytes to disk in the test, which is not hermetic or practical. The
// LimitedReader boundary check is a one-liner (n > maxModelBytes → error +
// os.Remove(tmp)) that is structurally straightforward to audit manually.
