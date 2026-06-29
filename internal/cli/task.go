package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/storage"
)

// noStatusLabel is the bucket label for a Task whose frontmatter carries no
// `status` field.
const noStatusLabel = "(no status)"

// statusOrder is the fixed leading order of status buckets in `task list`
// output. Any other status sorts alphabetically after these, and the no-status
// bucket is always last.
var statusOrder = []string{"in_progress", "todo", "done"}

// taskItem is one indexed Task document reduced to the fields `task list`
// renders: its uri (citation), title, and free-form (lowercased) status.
type taskItem struct {
	URI    string
	Title  string
	Status string
}

// taskGroup is the set of tasks that share a status, in display order.
type taskGroup struct {
	Status string
	Items  []taskItem
}

// newTaskCmd builds the `task` command and its read-only `list` action. It mints
// no IDs, enforces no state machine, and writes no files; it only reports the
// Task documents already in the index. It needs no allow_* gate.
func newTaskCmd(state *rootState) *cobra.Command {
	var collection, status string
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Inspect Task documents held in the index",
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List indexed Task documents grouped by status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTaskList(cmd, state, collection, status)
		},
	}
	list.Flags().StringVar(&collection, "collection", "", "restrict to a collection (exact match)")
	list.Flags().StringVar(&status, "status", "", "restrict to tasks with this status (case-insensitive)")
	cmd.AddCommand(list)

	return cmd
}

func runTaskList(cmd *cobra.Command, state *rootState, collection, status string) error {
	return withStore(state, false, func(a *app.App) error {
		docs, err := storage.ListDocuments(cmd.Context(), a.DB, storage.ListFilter{Collection: collection})
		if err != nil {
			return err
		}

		wantStatus := strings.ToLower(strings.TrimSpace(status))
		var items []taskItem
		for _, d := range docs {
			docType, st := taskMeta(d.FrontmatterJSON)
			if !strings.EqualFold(strings.TrimSpace(docType), "Task") {
				continue
			}
			st = strings.ToLower(strings.TrimSpace(st))
			if wantStatus != "" && st != wantStatus {
				continue
			}
			items = append(items, taskItem{URI: d.URI, Title: d.Title, Status: st})
		}

		renderTaskGroups(cmd.OutOrStdout(), groupTasks(items))

		return nil
	})
}

// taskMeta extracts the `type` and `status` fields from a document's stored
// frontmatter JSON. Missing or malformed frontmatter yields empty values.
func taskMeta(js string) (docType, status string) {
	if js == "" {
		return "", ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(js), &m) != nil {
		return "", ""
	}
	docType, _ = m["type"].(string)
	status, _ = m["status"].(string)

	return docType, status
}

// groupTasks buckets tasks by status and returns the buckets in a fixed order:
// in_progress, todo, done, then any other status alphabetically, then the
// no-status bucket last. Items within a group are ordered by uri. An empty
// status is bucketed under noStatusLabel.
func groupTasks(items []taskItem) []taskGroup {
	buckets := make(map[string][]taskItem)
	for _, it := range items {
		status := it.Status
		if strings.TrimSpace(status) == "" {
			status = noStatusLabel
		}
		buckets[status] = append(buckets[status], it)
	}

	seen := make(map[string]bool)
	order := make([]string, 0, len(buckets))
	for _, s := range statusOrder {
		if _, ok := buckets[s]; ok {
			order = append(order, s)
			seen[s] = true
		}
	}
	others := make([]string, 0, len(buckets))
	for s := range buckets {
		if s == noStatusLabel || seen[s] {
			continue
		}
		others = append(others, s)
	}
	slices.Sort(others)
	order = append(order, others...)
	if _, ok := buckets[noStatusLabel]; ok {
		order = append(order, noStatusLabel)
	}

	groups := make([]taskGroup, 0, len(order))
	for _, s := range order {
		g := buckets[s]
		slices.SortFunc(g, func(a, b taskItem) int { return strings.Compare(a.URI, b.URI) })
		groups = append(groups, taskGroup{Status: s, Items: g})
	}

	return groups
}

// renderTaskGroups prints each status as a header followed by its tasks' uri and
// title, aligned in a column.
func renderTaskGroups(out io.Writer, groups []taskGroup) {
	if len(groups) == 0 {
		_, _ = fmt.Fprintln(out, "no tasks")

		return
	}
	for _, g := range groups {
		_, _ = fmt.Fprintf(out, "%s (%d)\n", g.Status, len(g.Items))
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, it := range g.Items {
			_, _ = fmt.Fprintf(w, "  %s\t%s\n", it.URI, dash(it.Title))
		}
		_ = w.Flush()
	}
}
