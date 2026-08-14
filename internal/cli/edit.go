package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/tui"
)

// newEditCmd builds the `edit <uri>` command, which opens an OKF document in an
// interactive terminal editor: navigate its link graph, edit its frontmatter
// fields and its body, and reindex on save. It is gated by [mcp].allow_write.
func newEditCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "edit [uri]",
		Short: "Edit an OKF document in an interactive terminal editor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := ""
			if len(args) == 1 {
				uri = args[0]
			}

			return runEdit(cmd, state, uri)
		},
	}
}

func runEdit(cmd *cobra.Command, state *rootState, uri string) error {
	if strings.TrimSpace(uri) == "" {
		return errors.New("edit: provide a uri (e.g. mnemos edit tasks/ship.md); browsing the tree from the editor is not implemented yet")
	}

	a, err := state.loadApp()
	if err != nil {
		return err
	}
	// Reject before opening the store so the refusal never depends on a database
	// being present. The service re-checks the gate too (defense in depth).
	if !a.Config.MCP.AllowWrite {
		return errors.New("edit: editing is disabled; set [mcp].allow_write=true to enable")
	}
	if err = a.OpenStore(false); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()

	// The model loads the document up front, so a bad uri is reported on the
	// invoking terminal rather than inside an alternate screen.
	model, err := tui.NewEditModel(cmd.Context(), memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger), uri)
	if err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	if _, err = tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	return nil
}
