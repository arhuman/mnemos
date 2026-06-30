package cli

import (
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/mcp"
	"github.com/arhuman/mnemos/internal/security"
)

// newServeCmd builds the `serve` command, which runs the MCP server over the
// configured transport (stdio only for now). stdout is the MCP transport, so the
// command prints nothing to it; all diagnostics go through the slog logger on
// stderr.
func newServeCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server (stdio) exposing search, read, and context tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, state)
		},
	}
}

func runServe(cmd *cobra.Command, state *rootState) error {
	return withStore(state, false, func(a *app.App) error {
		if t := a.Config.MCP.Transport; t != "" && t != "stdio" {
			a.Logger.Warn("unsupported mcp transport, falling back to stdio", "transport", t)
		}

		// [capture].dir is validated and anchored to the tree root at load time
		// (config.Validate / CaptureLocation): an absolute value inside the tree is
		// accepted, one that escapes is rejected. No serve-local check is needed.

		// Build the retriever once for the server's lifetime. [search].use_vectors
		// selects hybrid lexical+vector retrieval; buildRetriever degrades to
		// lexical-only (with a stderr warning) when the binary lacks embedding
		// support or no model is installed, so serve always starts.
		retriever, err := buildRetriever(cmd, a, a.Config.Search.UseVectors)
		if err != nil {
			return err
		}
		srv := mcp.NewServer(a.DB, retriever, a.Config, a.TreeRoot(), security.NewRegexScanner(), a.Logger)

		if a.Config.MCP.AllowWrite {
			a.Logger.Info("mcp write-back enabled", "capture_dir", a.Layout.Capture)
		}
		if a.Config.MCP.AllowDelete {
			a.Logger.Info("mcp delete/move enabled", "tree_root", a.TreeRoot())
		}
		a.Logger.Info("mcp serve listening on stdio")
		if err := srv.Serve(cmd.Context(), &mcpsdk.StdioTransport{}); err != nil {
			return fmt.Errorf("serve: run mcp server: %w", err)
		}

		return nil
	})
}
