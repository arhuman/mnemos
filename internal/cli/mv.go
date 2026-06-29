package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/memory"
)

// newMvCmd builds the `mv <src> <dst>` command, which moves a file or directory
// within the OKF tree (rename on disk + re-index under the new path). It is gated
// by [mcp].allow_delete because the old index entries are deleted.
func newMvCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "mv <src> <dst>",
		Short: "Move a file or directory within the OKF tree (rename on disk and re-index)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMv(cmd, state, args[0], args[1])
		},
	}
}

func runMv(cmd *cobra.Command, state *rootState, src, dst string) error {
	a, err := state.loadApp()
	if err != nil {
		return err
	}
	// Reject before opening the store so the refusal never depends on a database
	// being present. The service re-checks the gate too (defense in depth).
	if !a.Config.MCP.AllowDelete {
		return errors.New("mv: move is disabled; set [mcp].allow_delete=true to enable")
	}
	if err = a.OpenStore(false); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()

	res, err := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger).Move(cmd.Context(), src, dst)
	if err != nil {
		return fmt.Errorf("mv: %w", err)
	}

	out := cmd.OutOrStdout()
	mv := res.Result
	if mv.IsDir {
		_, _ = fmt.Fprintf(out, "moved %s/ -> %s/ (%d files re-indexed)\n", res.From, res.To, len(mv.Entries))
	} else {
		docID := ""
		if len(mv.Entries) > 0 {
			docID = mv.Entries[0].DocumentID
		}
		_, _ = fmt.Fprintf(out, "moved %s -> %s (document %s)\n", res.From, res.To, docID)
	}
	if mv.DanglingLinks > 0 {
		_, _ = fmt.Fprintf(out, "warning: %d inbound link(s) still point at the old path (not rewritten)\n", mv.DanglingLinks)
	}

	return nil
}
