package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// The hook subcommands are the process-per-event entry points a Claude Code
// settings.json wires to SessionStart and UserPromptSubmit. They replace the
// former multi-process, jq-dependent shell pipelines (plan §4.1-4.2): one Go
// process reads the hook JSON from stdin, resolves the workspace, and either
// injects scoped markdown on stdout or stays completely silent.
//
// A hook must never break the session, so every failure mode — no workspace, an
// absent database, a malformed payload, a query error — resolves to empty output
// and a zero exit. That is why the RunE functions always return nil and the
// helpers return "" rather than an error: the shell wrappers no longer need
// `2>/dev/null || exit 0` to paper over failures, though they keep it as belt
// and braces.
const (
	// charsPerToken is the crude bytes-per-token ratio used to turn a
	// --max-tokens budget into a character cap. It is deliberately conservative
	// (real tokenizers average ~4 chars/token for English prose) so the cap errs
	// toward smaller, never larger, than the requested budget.
	charsPerToken = 4

	// hookDecisionLimit is how many recent decisions the working set injects.
	hookDecisionLimit = 5

	// hookRecallMaxPromptChars skips recall for prompts longer than this: a very
	// long prompt is almost always pasted material (a log, a file, a diff), not a
	// question worth a memory lookup.
	hookRecallMaxPromptChars = 2000
)

// hookInput is the subset of the Claude Code hook JSON payload the subcommands
// read from stdin. Unknown fields are ignored; a payload that is not JSON at all
// decodes to the zero value and the caller treats it as "nothing to do".
type hookInput struct {
	HookEventName string `json:"hook_event_name"`
	Source        string `json:"source"`
	Prompt        string `json:"prompt"`
	Cwd           string `json:"cwd"`
}

// newHookCmd builds the `hook` parent and its per-event subcommands. It is
// read-only (no allow_* gate) and mints nothing; it only reads the index.
func newHookCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Claude Code hook entry points (session-start, recall)",
		Long: "Process-per-event entry points for a Claude Code settings.json. Each reads " +
			"the hook JSON from stdin, resolves the workspace, injects scoped markdown on " +
			"stdout, and stays silent (exit 0, no output) when the project has no mnemos " +
			"workspace or nothing relevant to inject.",
	}
	cmd.AddCommand(newHookSessionStartCmd(state))
	cmd.AddCommand(newHookRecallCmd(state))

	return cmd
}

// newHookSessionStartCmd builds `hook session-start`: the SessionStart working
// set. It injects the open tasks and recent decisions of the resolved workspace,
// capped to --max-tokens, and prints nothing when no workspace resolves.
func newHookSessionStartCmd(state *rootState) *cobra.Command {
	var maxTokens int
	cmd := &cobra.Command{
		Use:   "session-start",
		Short: "Inject the scoped working set for a Claude Code SessionStart hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out := hookSessionStart(cmd, state, maxTokens); out != "" {
				_, _ = io.WriteString(cmd.OutOrStdout(), out)
			}

			return nil
		},
	}
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 1500, "approximate cap on injected context size in tokens (0 = uncapped)")

	return cmd
}

// newHookRecallCmd builds `hook recall`: the UserPromptSubmit lexical recall. It
// reads the prompt from the hook JSON, extracts salient terms, runs a lexical
// (never embedding-backed) search, and injects the top hits — or nothing when
// the prompt is not question-shaped, yields no terms, or matches nothing.
func newHookRecallCmd(state *rootState) *cobra.Command {
	var maxTokens, limit int
	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Inject cited recall for a Claude Code UserPromptSubmit hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out := hookRecall(cmd, state, limit, maxTokens); out != "" {
				_, _ = io.WriteString(cmd.OutOrStdout(), out)
			}

			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 3, "maximum number of recalled results")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 600, "approximate cap on injected context size in tokens (0 = uncapped)")

	return cmd
}

// hookSessionStart builds the working-set markdown, or "" when no workspace
// resolves (absent database), the store cannot open, or there is nothing to
// inject. Any error is swallowed: a hook stays silent rather than failing.
func hookSessionStart(cmd *cobra.Command, state *rootState, maxTokens int) string {
	var buf bytes.Buffer
	err := withStore(state, false, func(a *app.App) error {
		writeWorkingSet(cmd.Context(), &buf, a)

		return nil
	})
	if err != nil {
		return ""
	}

	return capTokens(buf.String(), maxTokens)
}

// writeWorkingSet appends the working set to buf: open tasks (in_progress/todo
// only, never done) and the most recent decisions. It writes nothing at all when
// both are empty, so an initialized-but-quiet workspace injects no noise. When
// the global store is in scope (§5.2) every line is labelled with its collection
// so a cross-project working set is not an unattributed pile of slugs.
func writeWorkingSet(ctx context.Context, buf *bytes.Buffer, a *app.App) {
	labelled := isGlobalScope(a)
	tasks := openTasks(ctx, a)
	decisions := recentDecisions(ctx, a, hookDecisionLimit)
	if len(tasks) == 0 && len(decisions) == 0 {
		return
	}

	_, _ = fmt.Fprintln(buf, "## mnemos working set")
	if labelled {
		_, _ = fmt.Fprintf(buf, "_global scope (%s); lines labelled by collection_\n", a.Layout.Source)
	}

	if len(tasks) > 0 {
		_, _ = fmt.Fprintln(buf, "\n### Open tasks")
		for _, t := range tasks {
			if labelled && t.Collection != "" {
				_, _ = fmt.Fprintf(buf, "- [%s] %s — %s (%s)\n", t.Status, dash(t.Title), t.Collection, t.URI)
			} else {
				_, _ = fmt.Fprintf(buf, "- [%s] %s (%s)\n", t.Status, dash(t.Title), t.URI)
			}
		}
	}

	if len(decisions) > 0 {
		_, _ = fmt.Fprintln(buf, "\n### Recent decisions")
		for _, d := range decisions {
			title := d.Title
			if strings.TrimSpace(title) == "" {
				title = d.URI
			}
			if labelled && d.Collection != "" {
				_, _ = fmt.Fprintf(buf, "- %s — %s (%s)\n", dash(title), d.Collection, d.URI)
			} else {
				_, _ = fmt.Fprintf(buf, "- %s (%s)\n", dash(title), d.URI)
			}
		}
	}
}

// openTasks returns the workspace's in_progress and todo Task documents, ordered
// in_progress first then by uri. The visibility denylist is applied so a hidden
// collection's tasks never surface into the working set. done/cancelled tasks are
// dropped: the working set is what is live, not a history.
func openTasks(ctx context.Context, a *app.App) []taskItem {
	docs, err := storage.ListDocuments(ctx, a.DB, storage.ListFilter{
		DocType:            "task",
		ExcludeCollections: a.Config.HiddenCollections(),
	})
	if err != nil {
		return nil
	}
	items := make([]taskItem, 0, len(docs))
	for _, d := range docs {
		_, st, title := taskMeta(d.FrontmatterJSON)
		status := strings.ToLower(strings.TrimSpace(st))
		if status != "in_progress" && status != "todo" {
			continue
		}
		if strings.TrimSpace(title) == "" {
			title = d.Title
		}
		items = append(items, taskItem{URI: d.URI, Collection: d.Collection, Title: title, Status: status})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Status != items[j].Status {
			return items[i].Status == "in_progress"
		}

		return items[i].URI < items[j].URI
	})

	return items
}

// recentDecisions lists up to limit indexed documents under the decisions/ path,
// most recently modified first. It goes through the memory service so the
// visibility denylist applies, mirroring the former `mnemos ls decisions` hook.
func recentDecisions(ctx context.Context, a *app.App, limit int) []browse.Entry {
	svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
	entries, err := svc.List(ctx, browse.Options{
		PathPrefix:  "decisions",
		IndexedOnly: true,
	})
	if err != nil {
		return nil
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].ModifiedAt > entries[j].ModifiedAt
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries
}

// hookRecall builds the recall markdown for a prompt read from stdin, or "" when
// the prompt is absent/oversized/not question-shaped, yields no salient terms,
// no workspace resolves, or the search returns nothing.
func hookRecall(cmd *cobra.Command, state *rootState, limit, maxTokens int) string {
	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return ""
	}
	prompt := parsePrompt(raw)
	if !shouldRecall(prompt) {
		return ""
	}
	terms := salientTerms(prompt)
	if len(terms) == 0 {
		return ""
	}

	var buf bytes.Buffer
	err = withStore(state, false, func(a *app.App) error {
		// The lexical engine only: recall runs in the prompt-submit hot path and
		// must never load the ONNX model. The memory service applies the
		// visibility denylist to the results.
		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		engine := search.NewEngine(a.DB, a.Logger)
		results, err := svc.Search(ctx(cmd), engine, search.Query{
			Text:  strings.Join(terms, " "),
			Limit: limit,
		})
		if err != nil || len(results) == 0 {
			return err
		}
		_, _ = fmt.Fprintln(&buf, "## mnemos recall (top hits — verify before use)")
		for i, r := range results {
			_, _ = fmt.Fprintf(&buf, "%d. %s (lines %d-%d)\n", i+1, citation(r), r.StartLine, r.EndLine)
		}

		return nil
	})
	if err != nil {
		return ""
	}

	return capTokens(buf.String(), maxTokens)
}

// ctx returns the command's context, or a background context when none is set
// (the cobra test harness does not always attach one).
func ctx(cmd *cobra.Command) context.Context {
	if c := cmd.Context(); c != nil {
		return c
	}

	return context.Background()
}

// parsePrompt extracts the prompt field from a hook JSON payload. A payload that
// is not JSON, or carries no prompt, yields "".
func parsePrompt(raw []byte) string {
	var in hookInput
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}

	return strings.TrimSpace(in.Prompt)
}

// shouldRecall gates whether a prompt is worth a memory lookup. It skips empty
// and oversized (pasted) input, then admits a prompt that either carries a recall
// cue phrase (the fast path) or is question-shaped. Cues are a fast path, not a
// requirement, since a scoped one-process lexical query is milliseconds cheap and
// recall stays silent on no hits regardless.
func shouldRecall(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if p == "" || len(p) > hookRecallMaxPromptChars {
		return false
	}
	if hasCuePhrase(p) {
		return true
	}

	return strings.HasSuffix(p, "?")
}

// recallCues are the phrases that mark a prompt as reaching for prior knowledge,
// in English and French. They mirror the phrases the former shell hook grepped
// for; matching is case-insensitive on the lowercased prompt.
var recallCues = []string{
	"as we decided", "what was", "what were", "remember when", "why did we",
	"the usual", "last time", "comme on avait", "c'etait quoi", "c'était quoi",
	"on avait décidé", "on avait decide",
}

// hasCuePhrase reports whether prompt contains any recall cue (case-insensitive).
func hasCuePhrase(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, cue := range recallCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}

	return false
}

// salientTerms reduces a prompt to the lowercased content words a lexical query
// should match on: it tokenizes on non-bareword runes, drops stopwords and very
// short tokens, deduplicates, and caps the count. Fewer, more salient terms make
// the engine's implicit-AND match more likely to return hits than the raw
// sentence would. The cap bounds the AND width for a long prompt.
func salientTerms(prompt string) []string {
	const maxTerms = 8
	fields := strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool {
		return !isTermRune(r)
	})
	seen := make(map[string]bool, len(fields))
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, "-.")
		if len(f) < 3 || stopwords[f] || seen[f] {
			continue
		}
		seen[f] = true
		terms = append(terms, f)
		if len(terms) == maxTerms {
			break
		}
	}

	return terms
}

// isTermRune reports whether r may appear inside a salient term: letters, digits,
// the inner-safe bareword characters, and any non-ASCII rune (so accented words
// survive). It mirrors the search engine's bareword rule so the terms this
// produces tokenize identically downstream.
func isTermRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	case r == '_' || r == '-' || r == '.':
		return true
	default:
		return r > 127
	}
}

// stopwords is the set of English and French function words dropped from a
// recall query. It is intentionally small — enough to strip the words that add
// nothing to a lexical match without needing IDF statistics.
var stopwords = map[string]bool{
	// English
	"the": true, "and": true, "for": true, "with": true, "was": true, "were": true,
	"are": true, "did": true, "does": true, "you": true, "our": true, "that": true,
	"this": true, "these": true, "those": true, "what": true, "when": true,
	"where": true, "why": true, "how": true, "which": true, "who": true, "them": true,
	"they": true, "his": true, "her": true, "its": true, "about": true, "into": true,
	"over": true, "than": true, "not": true, "can": true, "could": true, "would": true,
	"should": true, "will": true, "have": true, "has": true, "had": true, "get": true,
	"got": true, "use": true, "used": true, "from": true, "been": true, "being": true,
	// French
	"les": true, "des": true, "une": true, "que": true, "qui": true, "quoi": true,
	"avait": true, "est": true, "sont": true, "pour": true, "dans": true, "sur": true,
	"avec": true, "comme": true, "cette": true, "ces": true, "son": true, "ses": true,
	"nous": true, "vous": true, "ils": true, "elle": true, "elles": true, "aux": true,
	"par": true, "plus": true, "pas": true, "decide": true, "décidé": true,
}

// isGlobalScope reports whether the resolved workspace is the global default
// (~/.mnemos) rather than a project workspace. Only then is a working set a mix
// of projects that warrants per-line collection labelling.
func isGlobalScope(a *app.App) bool {
	return strings.HasPrefix(a.Layout.Source, "default")
}

// capTokens truncates s to an approximate token budget, cutting on a line
// boundary and appending a marker when it clips. A non-positive budget means no
// cap. The result is always valid UTF-8.
func capTokens(s string, maxTokens int) string {
	if maxTokens <= 0 {
		return s
	}
	limit := maxTokens * charsPerToken
	if len(s) <= limit {
		return s
	}
	truncated := s[:limit]
	if i := strings.LastIndexByte(truncated, '\n'); i > 0 {
		truncated = truncated[:i]
	}
	truncated = strings.ToValidUTF8(truncated, "")

	return truncated + "\n_[mnemos: truncated to ~" + strconv.Itoa(maxTokens) + " tokens]_\n"
}
