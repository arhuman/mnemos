package security

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegexScanner(t *testing.T) {
	scanner := NewRegexScanner()

	cases := []struct {
		name      string
		content   string
		wantRule  string // "" means expect no findings
		wantCount int
	}{
		{
			name:      "clean prose",
			content:   "the rules engine must stay pure with no I/O side effects",
			wantRule:  "",
			wantCount: 0,
		},
		{
			name:      "aws access key id",
			content:   "deploy with AKIAQYLPMN5HXYZ12345 in the env",
			wantRule:  "aws-access-key-id",
			wantCount: 1,
		},
		{
			name:      "private key block",
			content:   "key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBA\n-----END RSA PRIVATE KEY-----",
			wantRule:  "private-key-block",
			wantCount: 1,
		},
		{
			name:      "github token",
			content:   "token ghp_1234567890abcdefghijklmnopqrstuvwxyzAB leaked",
			wantRule:  "github-token",
			wantCount: 1,
		},
		{
			name:      "github fine-grained pat",
			content:   "pat github_pat_11ABCDEFG0abcdefghijKL_mnopqrstuvwxyz0123456789 here",
			wantRule:  "github-fine-grained-pat",
			wantCount: 1,
		},
		{
			name: "slack token",
			// Literal split so GitHub push protection does not flag this fake
			// fixture as a real Slack token; the scanner still matches the
			// assembled value at runtime. Do not re-join into one string.
			content:   "xox" + "b-1234567890-abcdefghijklmnop posted",
			wantRule:  "slack-token",
			wantCount: 1,
		},
		{
			name:      "generic credential assignment",
			content:   `config has api_key = "abcd1234efgh5678ijkl"`,
			wantRule:  "generic-credential-assignment",
			wantCount: 1,
		},
		{
			name:      "openai api key",
			content:   "key sk-proj-abcd1234efgh5678ijkl9012 leaked",
			wantRule:  "openai-api-key",
			wantCount: 1,
		},
		{
			name:      "anthropic api key",
			content:   "claude sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789ABCDEF here",
			wantRule:  "anthropic-api-key",
			wantCount: 1,
		},
		{
			name:      "huggingface token",
			content:   "hub hf_abcdefghijklmnopqrstuvwxyz1234 token",
			wantRule:  "huggingface-token",
			wantCount: 1,
		},
		{
			name:      "google api key",
			content:   "google AIzaabcdefghijklmnopqrstuvwxyz012345678 here",
			wantRule:  "google-api-key",
			wantCount: 1,
		},
		{
			name:      "stripe live key",
			content:   "stripe sk_live_abcd1234efgh5678ijkl key",
			wantRule:  "stripe-live-key",
			wantCount: 1,
		},
		{
			name:      "jwt",
			content:   "auth eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c bearer",
			wantRule:  "jwt",
			wantCount: 1,
		},
		{
			name:      "db dsn with inline credentials",
			content:   "DSN postgres://dbuser:s3cretpass@localhost:5432/app here",
			wantRule:  "db-dsn-credentials",
			wantCount: 1,
		},
		{
			name:      "benign prose mentioning key and token",
			content:   "the API key and the session token are described in the onboarding guide",
			wantRule:  "",
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := scanner.Scan(tc.content)
			require.NoError(t, err)
			require.Len(t, findings, tc.wantCount)
			if tc.wantRule != "" {
				require.Equal(t, tc.wantRule, findings[0].Rule)
				require.NotEmpty(t, findings[0].secret)
			}
		})
	}
}

func TestRegexScannerReportsMultipleFindings(t *testing.T) {
	scanner := NewRegexScanner()
	content := "AKIAQYLPMN5HXYZ12345 and ghp_1234567890abcdefghijklmnopqrstuvwxyzAB"

	findings, err := scanner.Scan(content)
	require.NoError(t, err)
	require.Len(t, findings, 2)
}
