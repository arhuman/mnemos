// Package security holds the guardrails that keep mnemos local-first and
// secret-free: the SecretScanner that screens captured content before it is
// written, and the path/exclude rules that bound what the agent can touch.
package security

import "regexp"

// Finding is one secret match in scanned content. Rule names the pattern that
// fired (e.g. "aws-access-key-id"). The matched substring is held on an
// unexported field so it cannot leak through serialization or an external
// caller: the remember tool reports only Rule names and never echoes the value
// back to the agent. In-package code (and tests) can still read it.
type Finding struct {
	// Rule is the identifier of the pattern that matched.
	Rule string
	// secret is the matched substring. It is intentionally unexported so the
	// raw credential cannot be surfaced to an untrusted client; it exists for
	// in-package auditing and tests only.
	secret string
}

// SecretScanner screens free-form content for credentials before it is
// persisted. It is the single swappable seam for secret detection: the regex
// implementation below can be replaced by a gitleaks-backed one without
// touching callers.
type SecretScanner interface {
	// Scan returns one Finding per detected secret. A clean input yields a nil
	// slice and a nil error. err is non-nil only on an operational failure, not
	// on the presence of a secret.
	Scan(content string) ([]Finding, error)
}

// rule pairs a regexp with the name reported when it matches. Patterns are
// compiled once at package load.
type rule struct {
	name string
	re   *regexp.Regexp
}

// rules lists the high-signal credential patterns screened on capture. The set
// is intentionally focused: it favors precision (few false positives on prose)
// over exhaustive coverage. Each pattern targets a credential shape that is
// unambiguous out of context, which is exactly the case for the bare text an
// agent hands to mnemos.remember.
var rules = []rule{
	{"aws-access-key-id", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"private-key-block", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`)},
	{"github-token", regexp.MustCompile(`gh[pousr]_[0-9A-Za-z]{36,}`)},
	{"github-fine-grained-pat", regexp.MustCompile(`github_pat_[0-9A-Za-z_]{20,}`)},
	{"slack-token", regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)},
	{"anthropic-api-key", regexp.MustCompile(`\bsk-ant-(?:api|admin)[0-9]{2}-[A-Za-z0-9_-]{20,}\b`)},
	{"huggingface-token", regexp.MustCompile(`\bhf_[A-Za-z0-9]{20,}\b`)},
	{"openai-api-key", regexp.MustCompile(`\bsk-(proj-)?[A-Za-z0-9_-]{20,}\b`)},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"stripe-live-key", regexp.MustCompile(`\b(sk|rk)_live_[0-9A-Za-z]{16,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{"db-dsn-credentials", regexp.MustCompile(`\b(postgres|postgresql|mysql|mongodb(\+srv)?)://[^:\s/]+:[^@\s]+@`)},
	{"generic-credential-assignment", regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token|password)\s*[:=]\s*['"][^'"]{16,}['"]`)},
}

// RegexScanner detects secrets with a fixed set of compiled patterns. It is the
// default SecretScanner: cgo-free, dependency-free, and tuned for the bare
// strings captured via mnemos.remember (where gitleaks' file-context heuristics
// do not fire). Construct with NewRegexScanner.
type RegexScanner struct{}

// NewRegexScanner returns the default regex-based secret scanner.
func NewRegexScanner() RegexScanner {
	return RegexScanner{}
}

// Scan reports every high-signal credential pattern found in content. Two
// distinct secrets on one line yield two findings; the same substring matched
// by multiple patterns is reported once, under the first (most specific) rule.
// The result is nil for clean content.
func (RegexScanner) Scan(content string) ([]Finding, error) {
	var findings []Finding
	seen := make(map[string]bool)
	for _, r := range rules {
		for _, m := range r.re.FindAllString(content, -1) {
			// Rules are ordered specific-first; skip a substring an earlier rule
			// already claimed so one secret is not double-reported under a broader
			// rule (e.g. an Anthropic key also matches the openai key shape).
			if seen[m] {
				continue
			}
			seen[m] = true
			findings = append(findings, Finding{Rule: r.name, secret: m})
		}
	}

	return findings, nil
}
