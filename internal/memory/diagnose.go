package memory

import (
	"context"

	"github.com/arhuman/mnemos/internal/doctor"
)

// Diagnose runs the read-only health detectors over the store and returns their
// findings. Like List (which wraps browse), it is a thin pass-through to the
// doctor package so the CLI `doctor` command and a future mnemos.doctor MCP tool
// share one implementation and cannot drift.
//
// Hidden collections are excluded here rather than in doctor: findings carry
// document URIs and tags, so an unfiltered run would disclose the namespace of a
// collection the operator denied. Applying it at the verb layer is what makes the
// exclusion hold for every surface, including one that does not exist yet.
//
// The tree root is supplied the same way, for the same reason: it enables the
// missing-file detector for every caller at once, so no surface silently runs
// without it.
func (s *Service) Diagnose(ctx context.Context, opts doctor.Options) ([]doctor.Finding, error) {
	opts.ExcludeCollections = s.cfg.HiddenCollections()
	opts.KBRoot = s.treeRoot

	return doctor.Run(ctx, s.db, opts)
}
