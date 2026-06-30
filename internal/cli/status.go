package cli

import (
	"context"
	"database/sql"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
)

func newStatusCmd(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show storage path, collection/document/chunk counts, and FTS availability",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, state)
		},
	}
}

// statusInfo is the gathered snapshot rendered by status.
type statusInfo struct {
	mnemosDir     string
	source        string
	kb            string
	dbPath        string
	collections   int
	documents     int
	chunks        int
	lastIndexedAt string
	ftsAvailable  bool
}

func runStatus(cmd *cobra.Command, state *rootState) error {
	// status is read-only (allowCreate=false): it must not create a database as a
	// side effect, and an absent/empty store surfaces an actionable error ("no
	// database …; run mnemos init") rather than the misleading zero-count report
	// it printed when opening silently created an empty database.
	return withStore(state, false, func(a *app.App) error {
		info, err := gatherStatus(cmd.Context(), a)
		if err != nil {
			return err
		}

		return renderStatus(cmd, info)
	})
}

func gatherStatus(ctx context.Context, a *app.App) (statusInfo, error) {
	info := statusInfo{
		mnemosDir: a.Layout.MnemosDir,
		source:    a.Layout.Source,
		kb:        a.Layout.KB,
		dbPath:    a.Layout.DB,
	}
	db := a.DB

	if err := db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT collection) FROM documents`).Scan(&info.collections); err != nil {
		return info, fmt.Errorf("status: count collections: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&info.documents); err != nil {
		return info, fmt.Errorf("status: count documents: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&info.chunks); err != nil {
		return info, fmt.Errorf("status: count chunks: %w", err)
	}

	var lastIndexed sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT MAX(indexed_at) FROM documents`).Scan(&lastIndexed); err != nil {
		return info, fmt.Errorf("status: max indexed_at: %w", err)
	}
	if lastIndexed.Valid {
		info.lastIndexedAt = lastIndexed.String
	} else {
		info.lastIndexedAt = "-"
	}

	info.ftsAvailable = ftsAvailable(ctx, db)

	return info, nil
}

// ftsAvailable reports whether the chunks_fts virtual table can be queried.
func ftsAvailable(ctx context.Context, db *sql.DB) bool {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks_fts`).Scan(&n)

	return err == nil
}

func renderStatus(cmd *cobra.Command, info statusInfo) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "mnemos dir\t%s (%s)\n", info.mnemosDir, info.source)
	_, _ = fmt.Fprintf(w, "kb root\t%s\n", info.kb)
	_, _ = fmt.Fprintf(w, "index db\t%s\n", info.dbPath)
	_, _ = fmt.Fprintf(w, "collections\t%d\n", info.collections)
	_, _ = fmt.Fprintf(w, "documents\t%d\n", info.documents)
	_, _ = fmt.Fprintf(w, "chunks\t%d\n", info.chunks)
	_, _ = fmt.Fprintf(w, "last indexed at\t%s\n", info.lastIndexedAt)
	_, _ = fmt.Fprintf(w, "fts available\t%t\n", info.ftsAvailable)

	return w.Flush()
}
