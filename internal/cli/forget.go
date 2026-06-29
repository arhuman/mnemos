package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/memory"
)

// newForgetCmd builds the `forget <path>` command, which deletes a file from the
// OKF tree (disk + index). It is gated by [mcp].allow_delete.
func newForgetCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <path>",
		Short: "Delete a file from the OKF tree (disk and index)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForget(cmd, state, args[0])
		},
	}
}

func runForget(cmd *cobra.Command, state *rootState, path string) error {
	a, err := state.loadApp()
	if err != nil {
		return err
	}
	// Reject before opening the store so the refusal never depends on a database
	// being present. The service re-checks the gate too (defense in depth).
	if !a.Config.MCP.AllowDelete {
		return errors.New("forget: delete is disabled; set [mcp].allow_delete=true to enable")
	}
	if err = a.OpenStore(false); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()

	res, err := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger).Forget(cmd.Context(), path)
	if err != nil {
		return fmt.Errorf("forget: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "forgot %s (deleted from disk: %t)\n", res.URI, res.Deleted)

	return nil
}
