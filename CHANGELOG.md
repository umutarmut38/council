# Changelog

All notable changes will be documented here.

This project follows semantic versioning once `v0.1.0` is tagged.

## v0.2.0 - 2026-06-07

Native Windows support.

### Added

- Native Windows pseudo-terminals via the ConPTY API
  (`internal/agent/pty_windows.go`). Agents now launch in real pseudo consoles
  on Windows instead of failing to start.
- npm-style `.cmd`/`.bat` agent shims (e.g. `claude`, `codex`) are launched
  through the command interpreter automatically, since `CreateProcess` cannot
  execute batch files directly.
- `internal/agent` tests covering Windows command-line construction and a real
  ConPTY spawn (output capture and exit code).

### Changed

- The agent session is now backed by a platform-neutral `ptyConn` interface:
  `creack/pty` on Unix, ConPTY on Windows. The reader and process waiter run
  concurrently so a session ends cleanly on both a Unix PTY (which reaches EOF)
  and a ConPTY pipe (which does not).
- Upgraded Bubble Tea to v1.3.7, restoring function-key input on the Windows
  console.
- Documentation now lists Windows as supported rather than experimental.

### Fixed

- The `F2` direct-input toggle now works on Windows (Bubble Tea v1.3.4 did not
  map function keys from the Windows console). `Ctrl+O` remains an alias.
- Orchestration worktree handling on Windows: git emits forward-slash paths
  while `filepath` uses backslashes, so worktree detection silently matched
  nothing. Git paths are now normalized to the host's native form, fixing
  worktree reuse, review, and adopt.

### Notes

- ConPTY requires Windows 10 1809 (build 17763) or newer. Windows Terminal is
  recommended; WSL remains a fine alternative.

## v0.1.0 - 2026-06-06

First public release candidate.

### Added

- Multi-pane PTY terminal UI for AI coding-agent CLIs.
- Broadcast, targeted messages, direct input, zoom, paging, overview, settings,
  transcript saving, and file-reference expansion.
- Plan → vote → build → review → adopt orchestration.
- Worker/reviewer roles, behavioral personalities, and target scopes.
- Per-repo config layering via `.council.yaml`.
- Resume support for interrupted plan, vote, build, and review phases.
- Release CI, tagged binary artifacts, and GitHub Pages documentation.

### Notes

- Native macOS and Linux are the primary targets for the first release.
- Windows binaries are built, but PTY behavior should be validated in Windows
  Terminal before declaring first-class support.
