package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/version"
)

// newVersionCmd prints the version. With -v/--verbose it adds the full build
// metadata (commit, build date, Go version) and whether semantic embeddings
// were compiled in (-tags embed). It loads no config or store.
func newVersionCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, or full build metadata with -v",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if !state.flags.verbose {
				_, err := fmt.Fprintf(out, "mnemos %s\n", version.Short())

				return err
			}
			_, err := fmt.Fprintf(out, "mnemos\n%s\nembeddings: %s\n", version.Info(), embeddingsStatus())

			return err
		},
	}
}

// embeddingsStatus reports whether the binary was built with semantic-embedding
// support, so `version -v` tells the user which retrieval modes are available.
func embeddingsStatus() string {
	if embed.Supported {
		return "enabled"
	}

	return "disabled (build with -tags embed)"
}
