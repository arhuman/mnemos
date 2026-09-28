package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arhuman/mnemos/internal/security"
	"github.com/arhuman/mnemos/internal/storage"
)

// ErrOriginReadOnly means the path names a document in a registered external
// origin. Those trees are indexed in place and stay canonical under their own
// tooling, so mnemos never writes to them (ADR-0013).
var ErrOriginReadOnly = errors.New("memory: registered origin is read-only")

// resolveWritable confines a caller-supplied path to the kb, refusing one that
// belongs to a registered external origin.
//
// The origin check runs BEFORE the confinement guard, and must: an origin uri is
// a plain relative path like "spec/note.md", so joining it to the kb root yields
// a path that is inside the kb and the guard accepts it. It would then resolve to
// a file that does not exist, which forget happily treats as "index-only
// deletion" and evicts the document. Relying on the guard alone would make an
// origin's index silently deletable.
func (s *Service) resolveWritable(ctx context.Context, path string) (abs, uri string, err error) {
	if o, ok := s.originFor(ctx, path); ok {
		return "", "", fmt.Errorf("%w: %q belongs to origin %q (%s); edit it with its own tooling, then run 'mnemos origin reindex %s'",
			ErrOriginReadOnly, path, o.Prefix, o.Path, o.Prefix)
	}

	return security.ResolveWithin(s.treeRoot, path, s.cfg.ConfinementExclude())
}

// originFor reports the registered origin owning a uri, if any. A storage failure
// is treated as "no origin": this only enriches an error that is already being
// returned, so it must never turn one failure into a different one.
func (s *Service) originFor(ctx context.Context, uri string) (storage.Origin, bool) {
	origins, err := storage.ListOrigins(ctx, s.db)
	if err != nil {
		return storage.Origin{}, false
	}
	clean := strings.TrimPrefix(strings.TrimSpace(uri), "./")
	for _, o := range origins {
		if clean == o.Prefix || strings.HasPrefix(clean, o.Prefix+"/") {
			return o, true
		}
	}

	return storage.Origin{}, false
}
