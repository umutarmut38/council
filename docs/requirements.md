---
title: Requirements
nav_order: 2
---

# Requirements and Platform Support

## Supported Platforms

| Platform | Status | Notes |
|---|---|---|
| macOS arm64/amd64 | Primary | Best tested. Works well in Terminal.app, iTerm2, and modern VS Code terminals (for colored pane borders in VS Code, set `terminal.integrated.customGlyphs: false` — see [Troubleshooting](troubleshooting.md#colors-work-in-one-terminal-but-not-another)). |
| Linux arm64/amd64 | Primary | Expected to work in modern terminal emulators with PTY support. |
| Windows amd64/arm64 | Supported | Native pseudo-terminals via the ConPTY API. Best in Windows Terminal; older consoles render less well. WSL remains a fine alternative. |

`council` uses pseudo-terminals to host child agents. On Windows this is the
ConPTY API, which requires Windows 10 1809 (build 17763) or newer — every
supported release of Windows 10/11 qualifies. Agent CLIs installed as
`.cmd`/`.bat` shims (the usual shape of npm-installed tools like `claude` or
`codex`) are launched through the command interpreter automatically; no extra
configuration is needed. Full-screen terminal behavior can still vary more than
on Unix, so prefer a modern terminal such as Windows Terminal.

## Build Requirements

- Go 1.23+
- Git 2.35+
- A terminal with ANSI color and Unicode support

## Runtime Requirements

- One or more agent CLIs installed and authenticated.
- `git` on `PATH` for orchestration.
- Write access to the repository for `.council/runs` and `.council/worktrees`.

Agent examples:

| Agent | Command example |
|---|---|
| Claude Code | `["claude"]` |
| OpenAI Codex | `["codex"]` |
| Cursor Agent | `["cursor-agent"]` |
| GitHub Copilot CLI | `["gh", "copilot"]` |
| opencode | `["opencode"]` |

## Agent-Specific Notes

- Some agents need a delay between text and Enter. Configure
  `terminal.submit_delay_ms: 250`.
- Some agents handle multi-line prompts more reliably with
  `terminal.send_mode: paste`.
- Tools that ask for folder trust may need pre-approval in each working
  directory or a phase-specific command that uses the tool's own safe
  auto-approval option.

## Orchestration Requirements

Orchestration must run inside a git repository because build and adopt use:

- `git worktree`
- `git diff`
- `git apply --3way`

The check command is project-specific:

```yaml
review:
  check_command: ["go", "test", "./..."]
```

Use `[]` for no gate during experimentation.
