// Package session stores per-run prompt, raw log, and transcript paths.
//
// It is intentionally small: orchestration owns workflow artifacts, while this
// package gives ordinary multiplexer sessions a durable place to write logs and
// user-visible transcripts.
package session
