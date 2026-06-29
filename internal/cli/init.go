package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/config"
)

func newInitCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the mnemos workspace (config, .mnemos/, database)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, state)
		},
	}
}

func runInit(cmd *cobra.Command, state *rootState) error {
	a, err := state.loadApp()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	// Write the default config in the cwd unless one already exists. With no
	// explicit --config, seed the conventional ./.mnemos.toml.
	configPath := state.flags.configPath
	if configPath == "" {
		configPath = config.FileName
	}
	if _, err := os.Stat(configPath); err == nil {
		a.Logger.Warn("config already exists, leaving it untouched", "path", configPath)
		_, _ = fmt.Fprintf(out, "kept existing config: %s\n", configPath)
	} else if os.IsNotExist(err) {
		if err = os.WriteFile(configPath, config.DefaultTOML(), 0o600); err != nil {
			return fmt.Errorf("init: write config %q: %w", configPath, err)
		}
		_, _ = fmt.Fprintf(out, "created config: %s\n", configPath)
	} else {
		return fmt.Errorf("init: stat config %q: %w", configPath, err)
	}

	// Create the database directory and the capture directory.
	dbDir := filepath.Dir(a.Config.Storage.Path)
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		return fmt.Errorf("init: create %q: %w", dbDir, err)
	}
	_, _ = fmt.Fprintf(out, "created directory: %s\n", dbDir)

	captureDir := a.Config.Capture.Dir
	if err := os.MkdirAll(captureDir, 0o750); err != nil {
		return fmt.Errorf("init: create %q: %w", captureDir, err)
	}
	_, _ = fmt.Fprintf(out, "created directory: %s\n", captureDir)

	// Open the database and run migrations.
	if err := a.OpenStore(true); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()
	_, _ = fmt.Fprintf(out, "initialized database: %s\n", a.Config.Storage.Path)

	return nil
}
