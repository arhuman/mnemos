package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/doctor"
)

func TestRenderFindingsEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderFindings(&buf, nil)
	require.Equal(t, "no issues found\n", buf.String())
}

func TestRenderFindingsGroupsAndSummarizes(t *testing.T) {
	findings := []doctor.Finding{
		// Deliberately out of category order to prove the render sorts them.
		{Category: doctor.CategoryStructure, Severity: doctor.SeverityInfo, Title: "guide/ has no index.md", URIs: []string{"guide/index.md"}},
		{Category: doctor.CategoryDuplicate, Severity: doctor.SeverityWarn, Title: "2 byte-identical documents", URIs: []string{"a.md", "b.md"}},
	}
	var buf bytes.Buffer
	renderFindings(&buf, findings)
	out := buf.String()

	require.Less(t, strings.Index(out, doctor.CategoryDuplicate), strings.Index(out, doctor.CategoryStructure),
		"duplicate group should render before structure group")
	require.Contains(t, out, "2 finding(s) across 2 categories: 1 warn, 1 info")
}

func TestFormatURIsTruncates(t *testing.T) {
	require.Equal(t, "-", formatURIs(nil))
	require.Equal(t, "a, b, c", formatURIs([]string{"a", "b", "c"}))
	require.Equal(t, "a, b, c (+2 more)", formatURIs([]string{"a", "b", "c", "d", "e"}))
}

func TestWriteJSONFindingsEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeJSONFindings(&buf, nil))
	require.Equal(t, "[]", strings.TrimSpace(buf.String()))
}

func TestWriteJSONFindingsRoundTrips(t *testing.T) {
	in := []doctor.Finding{{Category: doctor.CategoryOversized, Severity: doctor.SeverityInfo, Title: "big", URIs: []string{"big.md"}}}
	var buf bytes.Buffer
	require.NoError(t, writeJSONFindings(&buf, in))

	var got []doctor.Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, in, got)
}
