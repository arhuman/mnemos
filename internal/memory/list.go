package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/arhuman/mnemos/internal/browse"
)

// List walks the OKF tree on disk and annotates each file with the index
// metadata mnemos holds, applying opts as filters. It owns the indexed/unindexed
// mutual-exclusion check (formerly re-implemented on both surfaces) and the
// include/exclude/security-exclude wiring from config, so both surfaces narrow
// the tree identically. A nil result is normalized to an empty slice.
func (s *Service) List(ctx context.Context, opts browse.Options) ([]browse.Entry, error) {
	if opts.IndexedOnly && opts.UnindexedOnly {
		return nil, errors.New("indexed and unindexed are mutually exclusive")
	}

	entries, err := browse.List(
		ctx, s.db, s.treeRoot,
		s.cfg.Indexing.Include, s.cfg.Indexing.Exclude, s.cfg.SecurityExclude(),
		opts,
	)
	if err != nil {
		return nil, fmt.Errorf("memory: list: %w", err)
	}
	if entries == nil {
		entries = []browse.Entry{}
	}

	return entries, nil
}
