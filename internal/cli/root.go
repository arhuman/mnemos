// Package cli defines the mnemos cobra command tree.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
)

// flags holds the values bound to the root persistent flags, shared by all
// subcommands via the rootState.
type flags struct {
	configPath string
	mnemosDir  string
	verbose    bool
}

// rootState carries flag values into subcommand RunE functions and lazily
// builds the App.
type rootState struct {
	flags flags
}

// loadApp constructs the App from the current flag values. Subcommands call
// this in their RunE rather than relying on a pre-built global.
func (s *rootState) loadApp() (*app.App, error) {
	return app.Load(app.LoadOptions{
		ConfigPath: s.flags.configPath,
		MnemosDir:  s.flags.mnemosDir,
		Verbose:    s.flags.verbose,
	})
}

// withStore loads the App, opens its store, runs fn with the ready App, and
// closes the store afterward. allowCreate gates database creation: write
// commands (ingest, okfy, watch) pass true; read commands (search, ls, status,
// task, serve, reindex) pass false so a missing store is an actionable error
// rather than a silently-created empty one. It collapses the load -> open ->
// defer-close bootstrap that every store-backed command otherwise repeats.
//
// Commands that must reject before touching the store (e.g. the allow_delete
// gate on forget/mv) keep their own bootstrap so the early refusal still fires
// without opening a database.
func withStore(state *rootState, allowCreate bool, fn func(*app.App) error) error {
	a, err := state.loadApp()
	if err != nil {
		return err
	}
	if err := a.OpenStore(allowCreate); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()

	return fn(a)
}

// NewRootCmd builds the root command with its persistent flags and registers
// the Phase 0 subcommands.
func NewRootCmd() *cobra.Command {
	state := &rootState{}

	root := &cobra.Command{
		Use:           "mnemos",
		Short:         "Local, queryable, cited memory for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&state.flags.mnemosDir, "mnemos-dir", "", "explicit MNEMOS_DIR; overrides $MNEMOS_DIR, project ./.mnemos, and the ~/.mnemos default")
	root.PersistentFlags().StringVar(&state.flags.configPath, "config", "", "explicit mnemos.toml path; its directory becomes the MNEMOS_DIR")
	root.PersistentFlags().BoolVarP(&state.flags.verbose, "verbose", "v", false, "enable debug logging")

	root.AddCommand(newVersionCmd(state))
	root.AddCommand(newInitCmd(state))
	root.AddCommand(newStatusCmd(state))
	root.AddCommand(newIngestCmd(state))
	root.AddCommand(newAddCmd(state))
	root.AddCommand(newMigrateCmd(state))
	root.AddCommand(newSearchCmd(state))
	root.AddCommand(newLsCmd(state))
	root.AddCommand(newEvalCmd(state))
	root.AddCommand(newServeCmd(state))
	root.AddCommand(newWatchCmd(state))
	root.AddCommand(newModelsCmd(state))
	root.AddCommand(newReindexCmd(state))
	root.AddCommand(newForgetCmd(state))
	root.AddCommand(newMvCmd(state))
	root.AddCommand(newOkfyCmd(state))
	root.AddCommand(newValidateCmd(state))
	root.AddCommand(newTaskCmd(state))

	return root
}

// Execute runs the root command. It is the single entry point used by main.
func Execute() error {
	return NewRootCmd().Execute()
}
