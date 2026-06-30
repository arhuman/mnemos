package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/okf"
	"github.com/arhuman/mnemos/internal/security"
)

// maxRememberBytes caps the size of a single Remember note. A note is prose;
// 64 KiB is generous. Rejecting oversized input early avoids a disk write, the
// full ingest pipeline, and a secret scan over megabytes of text.
const maxRememberBytes = 64 * 1024

// errWriteDisabled and errDeleteDisabled are the gate refusals. The MCP tools
// are also un-registered when their gate is off (least capability); these are
// the authoritative checks the verb itself enforces.
var (
	errWriteDisabled  = errors.New("write disabled: set [mcp].allow_write=true")
	errDeleteDisabled = errors.New("delete disabled: set [mcp].allow_delete=true")
)

// RememberInput is a request to capture a note. Type is a free-form OKF type
// (e.g. idea, document, Task); Text is the note body; Collection defaults to
// "default"; Tags are optional frontmatter signals; Path, when set, is a
// tree-root-relative target ending in .md (else the note is auto-named under the
// capture directory).
type RememberInput struct {
	Type       string
	Text       string
	Collection string
	Tags       []string
	Path       string
}

// RememberResult reports the stored note's tree-relative uri (an immediate
// citation), the document id, the number of chunks indexed, the recorded type,
// and whether ingestion was deferred to a watcher (Deferred true means the file
// is written but not yet indexed, so DocumentID is empty and Chunks is 0).
type RememberResult struct {
	URI        string
	DocumentID string
	Chunks     int
	Type       string
	Deferred   bool
}

// Remember writes a note as an OKF markdown file and (unless capture is deferred
// to a watcher) indexes it. It validates the type/text, secret-scans the body
// before writing anything, confines a caller-chosen path to the tree, and
// appends a directory log entry. It is gated by [mcp].allow_write.
func (s *Service) Remember(ctx context.Context, in RememberInput) (RememberResult, error) {
	if err := ctx.Err(); err != nil {
		return RememberResult{}, err
	}
	if !s.cfg.MCP.AllowWrite {
		return RememberResult{}, errWriteDisabled
	}

	// OKF types are free-form ("never reject an unknown type"). Accept any
	// non-empty type so concepts like Task or Playbook can be captured; only an
	// empty/whitespace type is rejected.
	noteType := strings.TrimSpace(in.Type)
	if noteType == "" {
		return RememberResult{}, errors.New("type must be a non-empty OKF type (e.g. idea, document, Task)")
	}
	if strings.TrimSpace(in.Text) == "" {
		return RememberResult{}, errors.New("text must be non-empty")
	}
	if len(in.Text) > maxRememberBytes {
		return RememberResult{}, fmt.Errorf("text too large: %d bytes (max %d)", len(in.Text), maxRememberBytes)
	}

	// Secret-scan before writing anything. Report only the rule names; never echo
	// the matched secret back to the caller.
	findings, err := s.scanner.Scan(in.Text)
	if err != nil {
		return RememberResult{}, fmt.Errorf("remember scan: %w", err)
	}
	if len(findings) > 0 {
		return RememberResult{}, fmt.Errorf("rejected: detected secrets (%s); nothing written", strings.Join(findingRules(findings), ", "))
	}

	collection := in.Collection
	if collection == "" {
		collection = "default"
	}

	filename, content := ingest.RenderOKF(ingest.CaptureInput{
		Type:       noteType,
		Body:       in.Text,
		Tags:       in.Tags,
		Collection: collection,
		Timestamp:  time.Now(),
	})

	// outExisted tells the log whether this write created or updated a concept.
	absPath, uri, outExisted, err := s.writeNoteFile(in, filename, content)
	if err != nil {
		return RememberResult{}, err
	}

	// Record the change in the note's directory log.md. The concept is already
	// written, so a log-append failure is surfaced (wrapped) without losing it;
	// reserved files never get their own log entry. The capture path always
	// stamps the frontmatter timestamp to now (RenderOKF above), so timestamp
	// reliably means "last modified" for both create and update.
	if base := filepath.Base(absPath); !okf.IsReservedOKFFile(base) {
		kind := okf.LogCreation
		if outExisted {
			kind = okf.LogUpdate
		}
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if err = okf.AppendLog(filepath.Dir(absPath), kind, name, "./"+base, time.Now()); err != nil {
			return RememberResult{}, fmt.Errorf("remember log: %w", err)
		}
	}

	// defer_to_watcher: write-only mode. The OKF file is already on disk; a
	// watcher running over capture_dir is the sole ingest path, so we skip the
	// one-shot ingest and report the capture as deferred (no document_id, zero
	// chunks). This is the opt-in strict mode that avoids any chance of a double
	// index when a watcher is known to be running.
	if s.cfg.Capture.DeferToWatcher {
		s.logger.Info("remember captured note (deferred to watcher)", "uri", uri, "type", noteType)

		return RememberResult{URI: uri, Type: noteType, Chunks: 0, Deferred: true}, nil
	}

	// Default (one-shot) mode: remember both writes and ingests. This stays safe
	// even when a watcher is also running over capture_dir, because Phase 1
	// hash-skip makes the watcher's re-sighting of this just-ingested file a
	// no-op — there is no double index either way.
	docID, chunks, err := ingest.File(ctx, s.db, s.logger, absPath, uri, collection, chunk.ConfigFrom(s.cfg.Chunking.TargetTokens, s.cfg.Chunking.OverlapTokens))
	if err != nil {
		return RememberResult{}, fmt.Errorf("remember ingest: %w", err)
	}

	s.logger.Info("remember captured note", "uri", uri, "type", noteType, "chunks", chunks)

	return RememberResult{URI: uri, DocumentID: docID, Chunks: chunks, Type: noteType}, nil
}

// writeNoteFile writes the rendered note to its destination and reports the
// absolute path, the citation uri, and whether the destination already existed
// (so the caller's log records create vs update). A non-empty in.Path is a
// caller-chosen target confined to the tree and required to end in .md;
// otherwise the note is auto-named under the capture dir, cited by its
// capture-dir-relative path (e.g. ".mnemos/capture/idea-...md").
func (s *Service) writeNoteFile(in RememberInput, filename string, content []byte) (absPath, uri string, outExisted bool, err error) {
	if strings.TrimSpace(in.Path) == "" {
		// Anchor the capture dir to the tree root (not the process cwd) and derive
		// the citation URI from the tree-root-relative form, so an absolute
		// [capture].dir inside the tree writes to the right place and still cites
		// a tree-root-relative URI.
		absDir, relDir, derr := s.cfg.CaptureLocation(s.treeRoot)
		if derr != nil {
			return "", "", false, fmt.Errorf("remember capture dir: %w", derr)
		}
		dest := filepath.Join(absDir, filename)
		_, statErr := os.Stat(dest)
		absPath, err = ingest.WriteCapture(absDir, filename, content)
		if err != nil {
			return "", "", false, fmt.Errorf("remember write: %w", err)
		}

		return absPath, filepath.ToSlash(filepath.Join(relDir, filename)), statErr == nil, nil
	}

	// Caller-chosen target: confine it to the tree root and require .md so a
	// remembered note is always an OKF markdown file.
	abs, u, rerr := security.ResolveWithin(s.treeRoot, in.Path, s.cfg.ConfinementExclude())
	if rerr != nil {
		return "", "", false, fmt.Errorf("remember path: %w", rerr)
	}
	if !strings.EqualFold(filepath.Ext(abs), ".md") {
		return "", "", false, fmt.Errorf("path must end in .md, got %q", in.Path)
	}
	_, statErr := os.Stat(abs)
	absPath, err = ingest.WriteFileAtomic(abs, content)
	if err != nil {
		return "", "", false, fmt.Errorf("remember write: %w", err)
	}

	return absPath, u, statErr == nil, nil
}

// OkfyInput is a request to convert an existing plain .txt or .md file into an
// OKF document at Out and index it, leaving the source intact. Source and Out
// are tree-root-relative paths confined to the tree.
type OkfyInput struct {
	Source     string
	Out        string
	Collection string
	Type       string
	Tags       []string
	Force      bool
}

// Okfy converts a plain file into an OKF document and indexes it. It is not
// gated on either surface for the local operator running it directly; the MCP
// adapter applies the [mcp].allow_write gate at its boundary (the tool is also
// un-registered when write is off).
func (s *Service) Okfy(ctx context.Context, in OkfyInput) (ingest.OkfyResult, error) {
	if err := ctx.Err(); err != nil {
		return ingest.OkfyResult{}, err
	}

	return ingest.Okfy(ctx, s.db, s.logger, ingest.OkfyOptions{
		TreeRoot:   s.treeRoot,
		Exclude:    s.cfg.ConfinementExclude(),
		Source:     in.Source,
		Out:        in.Out,
		Collection: in.Collection,
		Type:       in.Type,
		Tags:       in.Tags,
		Force:      in.Force,
		Chunking:   chunk.ConfigFrom(s.cfg.Chunking.TargetTokens, s.cfg.Chunking.OverlapTokens),
		Scanner:    s.scanner,
	})
}

// ForgetResult reports the removed file's uri and whether a file was actually
// deleted from disk (false when it was already absent — forget is idempotent).
type ForgetResult struct {
	URI     string
	Deleted bool
}

// Forget removes a file from disk and the index. It de-indexes before touching
// disk (crash-coherent ordering, via ingest.ForgetPath). It is gated by
// [mcp].allow_delete. Resolve and ForgetPath errors are returned un-prefixed so
// each surface can add its own ("forget:" / "mcp:").
func (s *Service) Forget(ctx context.Context, path string) (ForgetResult, error) {
	if err := ctx.Err(); err != nil {
		return ForgetResult{}, err
	}
	if !s.cfg.MCP.AllowDelete {
		return ForgetResult{}, errDeleteDisabled
	}

	abs, uri, err := security.ResolveWithin(s.treeRoot, path, s.cfg.ConfinementExclude())
	if err != nil {
		return ForgetResult{}, err
	}

	deleted, err := ingest.ForgetPath(ctx, s.db, s.logger, abs, uri)
	if err != nil {
		return ForgetResult{}, err
	}

	return ForgetResult{URI: uri, Deleted: deleted}, nil
}

// MoveResult reports the old and new uris and the underlying ingest outcome (the
// re-indexed entries, whether the source was a directory, and any inbound links
// left dangling).
type MoveResult struct {
	From   string
	To     string
	Result ingest.MoveResult
}

// Move renames a file or directory within the OKF tree and re-indexes it under
// the new path, preserving each document's collection. It is gated by
// [mcp].allow_delete (the old index entries are deleted). The two resolve errors
// are tagged "source:" / "destination:" so a surface can prefix them
// ("mv: source:" / "mcp: destination:") and still tell the two apart.
func (s *Service) Move(ctx context.Context, from, to string) (MoveResult, error) {
	if err := ctx.Err(); err != nil {
		return MoveResult{}, err
	}
	if !s.cfg.MCP.AllowDelete {
		return MoveResult{}, errDeleteDisabled
	}

	exclude := s.cfg.ConfinementExclude()
	absFrom, oldURI, err := security.ResolveWithin(s.treeRoot, from, exclude)
	if err != nil {
		return MoveResult{}, fmt.Errorf("source: %w", err)
	}
	absTo, newURI, err := security.ResolveWithin(s.treeRoot, to, exclude)
	if err != nil {
		return MoveResult{}, fmt.Errorf("destination: %w", err)
	}

	res, err := ingest.MovePath(ctx, s.db, s.logger, absFrom, absTo, oldURI, newURI, chunk.ConfigFrom(s.cfg.Chunking.TargetTokens, s.cfg.Chunking.OverlapTokens))
	if err != nil {
		return MoveResult{}, err
	}

	return MoveResult{From: oldURI, To: newURI, Result: res}, nil
}

// findingRules returns the sorted, de-duplicated rule names from findings. It
// never includes the matched secret values.
func findingRules(findings []security.Finding) []string {
	seen := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		seen[f.Rule] = struct{}{}
	}
	rules := make([]string, 0, len(seen))
	for r := range seen {
		rules = append(rules, r)
	}
	slices.Sort(rules)

	return rules
}
