package session

import (
	"regexp"
	"sort"
	"strings"
)

// Optional secret redaction for saved transcripts and a scanner that locates
// the same patterns for `council artifacts scan`. Patterns target common
// high-entropy credential formats; matching is best-effort and intentionally
// conservative so prose and code are not mangled.

var redactEnabled = false

// SetRedact enables redaction of common secret patterns in saved transcripts.
// Raw PTY logs are written as a live stream and are not redacted; keep
// sessions.private on (the default) to protect them.
func SetRedact(enabled bool) { redactEnabled = enabled }

// secretPattern is a named credential pattern, shared by Redact (which masks
// matches) and Scan (which reports their locations).
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	// AWS access key IDs and the secret that usually travels next to them.
	{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"AWS secret key", regexp.MustCompile(`(?i)\baws_?secret[_a-z]*\s*[:=]\s*\S+`)},
	// GitHub tokens (classic and fine-grained).
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	// OpenAI / Anthropic / Slack / Google styles.
	{"OpenAI-style key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	// Bearer headers and generic key=value credential assignments.
	{"Bearer token", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{16,}=*`)},
	{"credential assignment", regexp.MustCompile(`(?i)\b(?:api[_-]?key|auth[_-]?token|access[_-]?token|client[_-]?secret|password)\s*[:=]\s*['"]?[^\s'"]{8,}['"]?`)},
	// PEM private key blocks.
	{"private key block", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)},
}

// Redact replaces common secret patterns in text with [REDACTED].
func Redact(text string) string {
	for _, p := range secretPatterns {
		text = p.re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

// SecretFinding locates one likely secret in scanned text. It never carries the
// secret value itself — only its kind and 1-based line number — so scan output
// is safe to print and share.
type SecretFinding struct {
	Line int
	Kind string
}

// Scan reports likely secrets in text using the same patterns as Redact. It is
// the engine behind `council artifacts scan` and the pre-report/pre-pr warning.
// Findings are sorted by line, then kind, for stable output.
func Scan(text string) []SecretFinding {
	var out []SecretFinding
	for _, p := range secretPatterns {
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			out = append(out, SecretFinding{
				Line: 1 + strings.Count(text[:loc[0]], "\n"),
				Kind: p.name,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
