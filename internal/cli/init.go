package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/workspace"
)

func newInitCmd(state *rootState) *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a mnemos workspace (MNEMOS_DIR: config, kb/, state, models)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, state, global)
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "initialize the global ~/.mnemos workspace instead of a local ./.mnemos")

	return cmd
}

// initTarget decides which MNEMOS_DIR `init` creates. An explicit
// --mnemos-dir/--config wins; otherwise --global selects ~/.mnemos and the
// default is a project-local ./.mnemos in the current directory.
func initTarget(f flags, global bool, home, cwd string) (string, error) {
	switch {
	case f.mnemosDir != "":
		return filepath.Abs(f.mnemosDir)
	case f.configPath != "":
		abs, err := filepath.Abs(f.configPath)
		if err != nil {
			return "", err
		}

		return filepath.Dir(abs), nil
	case global:
		if home == "" {
			return "", errors.New("init: --global needs a home directory")
		}

		return filepath.Join(home, workspace.DirName), nil
	default:
		return filepath.Join(cwd, workspace.DirName), nil
	}
}

func runInit(cmd *cobra.Command, state *rootState, global bool) error {
	home, _ := os.UserHomeDir()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("init: getwd: %w", err)
	}

	target, err := initTarget(state.flags, global, home, cwd)
	if err != nil {
		return err
	}
	layout := workspace.New(target)
	if state.flags.configPath != "" {
		if layout.Config, err = filepath.Abs(state.flags.configPath); err != nil {
			return err
		}
	}

	out := cmd.OutOrStdout()
	logger := app.NewLogger(state.flags.verbose)

	if err = os.MkdirAll(layout.MnemosDir, 0o750); err != nil {
		return fmt.Errorf("init: create %q: %w", layout.MnemosDir, err)
	}

	// Seed the config unless one already exists.
	if _, statErr := os.Stat(layout.Config); statErr == nil {
		logger.Warn("config already exists, leaving it untouched", "path", layout.Config)
		_, _ = fmt.Fprintf(out, "kept existing config: %s\n", layout.Config)
	} else if os.IsNotExist(statErr) {
		if err = os.WriteFile(layout.Config, config.DefaultTOML(), 0o600); err != nil {
			return fmt.Errorf("init: write config %q: %w", layout.Config, err)
		}
		_, _ = fmt.Fprintf(out, "created config: %s\n", layout.Config)
	} else {
		return fmt.Errorf("init: stat config %q: %w", layout.Config, statErr)
	}

	// Create the knowledge base (with its capture subdir), the index-db parent,
	// and the models directory.
	for _, dir := range []string{layout.Capture, filepath.Dir(layout.DB), layout.Models} {
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("init: create %q: %w", dir, err)
		}
		_, _ = fmt.Fprintf(out, "created directory: %s\n", dir)
	}

	cfg, err := config.Load(layout.Config, func(p string) bool {
		info, statErr := os.Stat(p)

		return statErr == nil && !info.IsDir()
	})
	if err != nil {
		return err
	}

	a := &app.App{Config: cfg, Layout: layout, Logger: logger}
	if err := a.OpenStore(true); err != nil {
		return err
	}
	defer func() { _ = a.Close() }()
	_, _ = fmt.Fprintf(out, "initialized database: %s\n", layout.DB)

	return nil
}
