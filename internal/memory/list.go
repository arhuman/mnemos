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

	return hideCollections(entries, s.cfg.HiddenCollections()), nil
}

// hideCollections drops entries whose collection is in the deny list — the
// server-side visibility boundary applied to list/ls, matching the query-layer
// exclusion the search path applies. An un-indexed file has no collection
// attribution, so it is never dropped here (there is nothing hidden to leak). A
// nil result is normalized to an empty slice.
func hideCollections(entries []browse.Entry, deny []string) []browse.Entry {
	if len(deny) == 0 {
		if entries == nil {
			return []browse.Entry{}
		}

		return entries
	}
	hidden := make(map[string]bool, len(deny))
	for _, c := range deny {
		hidden[c] = true
	}
	out := make([]browse.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Collection != "" && hidden[e.Collection] {
			continue
		}
		out = append(out, e)
	}

	return out
}
