package memory

import (
	"context"

	"github.com/arhuman/mnemos/internal/doctor"
)

// Diagnose runs the read-only health detectors over the store and returns their
// findings. Like List (which wraps browse), it is a thin pass-through to the
// doctor package so the CLI `doctor` command and a future mnemos.doctor MCP tool
// share one implementation and cannot drift.
func (s *Service) Diagnose(ctx context.Context, opts doctor.Options) ([]doctor.Finding, error) {
	return doctor.Run(ctx, s.db, opts)
}
