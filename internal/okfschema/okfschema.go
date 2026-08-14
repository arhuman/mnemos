// Package okfschema is the field-type registry an editor consults to know how a
// frontmatter key may be edited: which keys are closed enumerations, which hold
// tag lists, and which the indexer owns and a human must not retype. It is pure
// data with no store or UI dependency, so other surfaces (a future validate
// check) can reuse the same definitions.
package okfschema

import (
	"slices"
	"strings"
)

// FieldKind is how an editor presents a frontmatter field.
type FieldKind int

const (
	// KindText is a free-form single-line scalar.
	KindText FieldKind = iota
	// KindEnum is a scalar restricted to FieldSchema.Enum.
	KindEnum
	// KindTags is a list of strings, retyped as a whole.
	KindTags
	// KindReadOnly is a field the index or the pipeline owns; it is displayed
	// but never edited.
	KindReadOnly
)

// FieldSchema describes one frontmatter key. Enum is populated only for
// KindEnum.
type FieldSchema struct {
	Key  string
	Kind FieldKind
	Enum []string
}

// TypeSchema is the set of known fields for one OKF document type.
type TypeSchema struct {
	Fields []FieldSchema
}

// registry holds the per-type field definitions. The task entry mirrors
// skills/mnemos-okf/references/task-schema.md; the two are kept in sync by hand.
var registry = map[string]TypeSchema{
	"task": {Fields: []FieldSchema{
		{Key: "status", Kind: KindEnum, Enum: []string{"backlog", "todo", "in_progress", "done", "cancelled"}},
		{Key: "priority", Kind: KindEnum, Enum: []string{"low", "medium", "high", "critical"}},
		{Key: "title", Kind: KindText},
		{Key: "tags", Kind: KindTags},
	}},
}

// systemKeys are written by the ingest pipeline or address the document itself;
// retyping one in an editor would desynchronize it from the index.
var systemKeys = []string{"type", "collection", "timestamp"}

// FieldSchemaFor returns how key should be edited on a document of docType. An
// unknown type or an unlisted key falls back on the key alone: tag lists for
// "tags", read-only for the system keys, free text otherwise — so an editor
// always has a usable answer and never rejects an unknown OKF type.
func FieldSchemaFor(docType, key string) FieldSchema {
	if ts, ok := registry[strings.ToLower(strings.TrimSpace(docType))]; ok {
		for _, f := range ts.Fields {
			if f.Key == key {
				return f
			}
		}
	}
	switch {
	case key == "tags":
		return FieldSchema{Key: key, Kind: KindTags}
	case slices.Contains(systemKeys, key):
		return FieldSchema{Key: key, Kind: KindReadOnly}
	default:
		return FieldSchema{Key: key, Kind: KindText}
	}
}

// Cycle returns the enum value delta steps from current, wrapping around. A
// current value outside the enum (or a non-enum field) resolves to the first
// value, so an editor can always offer a valid choice.
func (f FieldSchema) Cycle(current string, delta int) string {
	if f.Kind != KindEnum || len(f.Enum) == 0 {
		return current
	}
	i := slices.Index(f.Enum, current)
	if i < 0 {
		return f.Enum[0]
	}
	n := len(f.Enum)

	return f.Enum[((i+delta)%n+n)%n]
}
