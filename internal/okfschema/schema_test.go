package okfschema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/okfschema"
)

// TestFieldSchemaFor covers the registry hit for a known type and every fallback
// arm an unknown type or key lands on.
func TestFieldSchemaFor(t *testing.T) {
	cases := []struct {
		name    string
		docType string
		key     string
		want    okfschema.FieldKind
		enum    []string
	}{
		{"task status is an enum", "task", "status", okfschema.KindEnum, []string{"backlog", "todo", "in_progress", "done", "cancelled"}},
		{"task priority is an enum", "task", "priority", okfschema.KindEnum, []string{"low", "medium", "high", "critical"}},
		{"type is matched case-insensitively", "Task", "status", okfschema.KindEnum, []string{"backlog", "todo", "in_progress", "done", "cancelled"}},
		{"task title is free text", "task", "title", okfschema.KindText, nil},
		{"task tags is a list", "task", "tags", okfschema.KindTags, nil},
		{"unknown key on a known type falls back", "task", "started", okfschema.KindText, nil},
		{"tags falls back to a list on an unknown type", "playbook", "tags", okfschema.KindTags, nil},
		{"type is read-only", "playbook", "type", okfschema.KindReadOnly, nil},
		{"collection is read-only", "playbook", "collection", okfschema.KindReadOnly, nil},
		{"timestamp is read-only", "playbook", "timestamp", okfschema.KindReadOnly, nil},
		{"status on an unknown type is free text", "playbook", "status", okfschema.KindText, nil},
		{"empty type falls back", "", "title", okfschema.KindText, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := okfschema.FieldSchemaFor(tc.docType, tc.key)
			require.Equal(t, tc.key, got.Key)
			require.Equal(t, tc.want, got.Kind)
			require.Equal(t, tc.enum, got.Enum)
		})
	}
}

// TestCycle covers enum stepping in both directions, the wrap-around at each
// end, and the two arms that fall back to a usable value.
func TestCycle(t *testing.T) {
	status := okfschema.FieldSchemaFor("task", "status")

	require.Equal(t, "done", status.Cycle("in_progress", 1))
	require.Equal(t, "todo", status.Cycle("in_progress", -1))
	require.Equal(t, "backlog", status.Cycle("cancelled", 1), "cycling past the end wraps")
	require.Equal(t, "cancelled", status.Cycle("backlog", -1), "cycling before the start wraps")
	require.Equal(t, "backlog", status.Cycle("bogus", 1), "an off-enum value resolves to the first")

	text := okfschema.FieldSchemaFor("task", "title")
	require.Equal(t, "unchanged", text.Cycle("unchanged", 1), "a non-enum field does not cycle")
}
