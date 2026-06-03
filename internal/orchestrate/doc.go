// Package orchestrate implements the file-backed council workflow.
//
// The controller coordinates plan, vote, build, review, adopt, and resume. Runs
// are stored as Markdown/JSON artifacts under .council/runs so users can inspect
// or recover them without special tooling. Build implementations happen in git
// worktrees, while voting and review use persisted anonymized assignments to
// prevent self-bias and keep resume deterministic.
package orchestrate
