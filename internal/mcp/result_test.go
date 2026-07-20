package mcp

import (
	"encoding/json"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// TestResultModes covers the three wire shapes result() can produce. Out must
// always be nil (so the SDK emits the result untouched), and each mode sets only
// the fields it should: text → Content only, structured → StructuredContent only,
// both → both.
func TestResultModes(t *testing.T) {
	type payload struct {
		N int `json:"n"`
	}

	cases := []struct {
		mode           resultMode
		wantContent    bool
		wantStructured bool
	}{
		{resultText, true, false},
		{resultStructured, false, true},
		{resultBoth, true, true},
		{resultMode(""), true, false}, // zero value behaves as text
	}
	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			s := &Server{resultMode: tc.mode}
			res, out, err := s.result(payload{N: 7})
			require.NoError(t, err)
			require.Nil(t, out, "Out must be nil so the SDK emits the result untouched")

			if tc.wantContent {
				require.Len(t, res.Content, 1)
				text, ok := res.Content[0].(*mcpsdk.TextContent)
				require.True(t, ok, "content block must be text")
				var got payload
				require.NoError(t, json.Unmarshal([]byte(text.Text), &got))
				require.Equal(t, 7, got.N)
			} else {
				require.Empty(t, res.Content)
			}

			if tc.wantStructured {
				require.NotNil(t, res.StructuredContent)
			} else {
				require.Nil(t, res.StructuredContent)
			}
		})
	}
}
