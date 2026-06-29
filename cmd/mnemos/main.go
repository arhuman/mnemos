// Command mnemos is the CLI entry point for the local memory engine.
package main

import (
	"log/slog"
	"os"

	"github.com/arhuman/mnemos/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
