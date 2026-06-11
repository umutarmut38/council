package session

import "regexp"

// Optional secret redaction for saved transcripts. Patterns target common
// high-entropy credential formats; matching is best-effort and intentionally
// conservative so prose and code are not mangled.

var redactEnabled = false

// SetRedact enables redaction of common secret patterns in saved transcripts.
// Raw PTY logs are written as a live stream and are not redacted; keep
// sessions.private on (the default) to protect them.
func SetRedact(enabled bool) { redactEnabled = enabled }

var secretPatterns = []*regexp.Regexp{
	// AWS access key IDs and the secret that usually travels next to them.
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\baws_?secret[_a-z]*\s*[:=]\s*\S+`),
	// GitHub tokens (classic and fine-grained).
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`),
	// OpenAI / Anthropic / Slack / Google styles.
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	// Bearer headers and generic key=value credential assignments.
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]{16,}=*`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|auth[_-]?token|access[_-]?token|client[_-]?secret|password)\s*[:=]\s*['"]?[^\s'"]{8,}['"]?`),
	// PEM private key blocks.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
}

// Redact replaces common secret patterns in text with [REDACTED].
func Redact(text string) string {
	for _, p := range secretPatterns {
		text = p.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
