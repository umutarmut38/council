# Changelog

All notable changes will be documented here.

This project follows semantic versioning once `v0.1.0` is tagged.

## v0.1.0 - Unreleased

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
