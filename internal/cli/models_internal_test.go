package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/embed"
)

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("model-bytes"))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	dest := filepath.Join(t.TempDir(), "model.onnx")
	require.NoError(t, downloadFile(context.Background(), client, srv.URL, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "model-bytes", string(got))
}

func TestDownloadFileNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	dest := filepath.Join(t.TempDir(), "model.onnx")
	err := downloadFile(context.Background(), client, srv.URL, dest)
	require.Error(t, err)
	require.NoFileExists(t, dest)
}

func TestRunModelsInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	// Point the default model's downloads at the test server and install into a
	// temp HOME so no network or real weights are touched.
	orig := modelDownloads[embed.DefaultModel]
	modelDownloads[embed.DefaultModel] = []modelFile{
		{url: srv.URL + "/model.onnx", name: "model.onnx"},
		{url: srv.URL + "/tokenizer.json", name: "tokenizer.json"},
	}
	t.Cleanup(func() { modelDownloads[embed.DefaultModel] = orig })
	t.Setenv("HOME", t.TempDir())

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runModelsInstall(cmd, embed.DefaultModel))

	dir, err := embed.ModelDir(embed.DefaultModel)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(dir, "model.onnx"))
	require.FileExists(t, filepath.Join(dir, "tokenizer.json"))
	require.Contains(t, out.String(), "installed")
}

func TestRunModelsInstallUnknown(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	err := runModelsInstall(cmd, "not-a-model")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown model")
}

func TestLoadEmbedderModelMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no model installed here
	_, err := loadEmbedder()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not installed")
}
