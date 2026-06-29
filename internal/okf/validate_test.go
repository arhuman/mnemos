package okf

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// codes returns the sorted set of issue codes in a report.
func codes(rep Report) []string {
	out := make([]string, 0, len(rep.Issues))
	for _, iss := range rep.Issues {
		out = append(out, iss.Code)
	}
	slices.Sort(out)

	return out
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		bundle    string
		wantCodes []string
		wantOK    bool
	}{
		{name: "conformant", bundle: "testdata/good", wantCodes: []string{}, wantOK: true},
		{name: "E1 no frontmatter", bundle: "testdata/e1", wantCodes: []string{"E1"}, wantOK: false},
		{name: "E2 missing type", bundle: "testdata/e2", wantCodes: []string{"E2"}, wantOK: false},
		{name: "E3 reserved file", bundle: "testdata/e3", wantCodes: []string{"E3"}, wantOK: false},
		{name: "W1 missing title/description", bundle: "testdata/w1", wantCodes: []string{"W1"}, wantOK: true},
		{name: "W2 broken cross-link", bundle: "testdata/w2", wantCodes: []string{"W2"}, wantOK: true},
		{name: "W3 missing timestamp", bundle: "testdata/w3", wantCodes: []string{"W3"}, wantOK: true},
		{name: "W4 dir without index", bundle: "testdata/w4", wantCodes: []string{"W4"}, wantOK: true},
		{name: "W5 bad log dates", bundle: "testdata/w5", wantCodes: []string{"W5"}, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := Validate(context.Background(), tc.bundle)
			require.NoError(t, err)
			require.Equal(t, tc.wantCodes, codes(rep), "issue codes")
			require.Equal(t, tc.wantOK, rep.OK(), "conformance")
		})
	}
}

func TestValidateMissingBundle(t *testing.T) {
	_, err := Validate(context.Background(), "testdata/does-not-exist")
	require.Error(t, err)
}

func TestDatesISO8601Descending(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "descending", content: "## 2024-03-01\n## 2024-01-01\n", want: true},
		{name: "ascending", content: "## 2024-01-01\n## 2024-03-01\n", want: false},
		{name: "no date headings", content: "## Release notes\n## Other\n", want: false},
		{name: "empty", content: "# Log\n", want: false},
		{name: "date with trailing text", content: "## 2024-03-01 release\n## 2024-02-01 patch\n", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, datesISO8601Descending([]byte(tc.content)))
		})
	}
}
