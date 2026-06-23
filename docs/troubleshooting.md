---
title: Troubleshooting
nav_order: 9
---

# Troubleshooting

## An Agent Did Not Receive a Prompt

Increase startup delay:

```yaml
ui:
  initial_prompt_delay_ms: 8000
```

If the agent receives text but does not submit it, send Enter separately:

```yaml
terminal:
  submit_sequence: cr
  submit_delay_ms: 250
```

If multi-line prompts are malformed, try bracketed paste:

```yaml
terminal:
  send_mode: paste
  before_send_sequence: ctrl+u
  submit_sequence: cr
```

## Output Looks Broken

Try pane-sized PTYs and color:

```yaml
terminal:
  renderer: screen
  pty_size: pane
  resize: true
  color: true
```

If a specific tool renders poorly in screen mode, use transcript mode:

```yaml
terminal:
  renderer: transcript
```

## Windows-Specific Notes

`council` hosts agents with the Windows ConPTY API, available on Windows 10 1809
(build 17763) and newer.

- Run inside **Windows Terminal** for the best rendering. The legacy
  `conhost.exe` console window handles full-screen TUIs less gracefully; if
  output looks garbled, switch terminals or try `renderer: transcript`.
- npm-installed agent CLIs are usually `.cmd` shims (`claude.cmd`, `codex.cmd`).
  These are launched through the command interpreter automatically. If
  `council doctor` reports an agent as not found, confirm it resolves on `PATH`
  (`where claude`).
- If a child fails to start with `ConPty is not available`, your Windows build
  predates 1809. Use WSL instead.

## Tool Asks to Trust a Worktree Folder

Plan, vote, and review run in the launch directory. Build runs in per-agent git
worktrees. Some agent tools ask for trust in every new directory.

Options:

- Trust `.council/worktrees/<run>/<agent>` once in that tool.
- Use a phase-specific command with the tool's own safe auto-approval option.
- Keep the agent as `role: [reviewer]` if it should never build.

## `/resume` Reopens the Wrong Place

Current runs write `state.json` while a phase is active. Older runs without
state are inferred from artifacts:

- no plans → resume plan
- plans but no vote result → resume vote
- vote result and build base → resume build
- otherwise reopen as context

Use `/status` to see the active run and phase.

## `npm run dev` Fails After `/adopt`

`/adopt` applies files; it does not install dependencies. For a Node example:

```bash
cd examples/my-app
npm ci
npm run dev
```

If a sandboxed environment blocks binding `::1`, bind explicitly:

```bash
npm run dev -- --host 127.0.0.1
```

## Worktrees Are Stale

Clean council-managed worktrees:

```text
/clean
```

or:

```bash
council clean
```

This removes `.council/worktrees/<run>/<agent>` and the corresponding
`council/<agent>/<timestamp>` branches.

## Colors work in one terminal but not another

Agent border colors — and the whole [theme](configuration.md#themes) — are
emitted as plain indexed SGR (`38;5;N`), the most widely supported color
encoding there is. If they still don't show in a specific terminal, run
`council doctor` there: it prints three test rows (colored text, colored blocks,
colored borders) in the active theme's colors.

- All three rows colored: the terminal is fine; check that the session is
  running the binary you think it is.
- TEXT colored but BLOCKS/BORDERS not: the terminal draws box/block glyphs
  itself and its renderer is dropping the color. **In VS Code the fix is:**

  ```jsonc
  "terminal.integrated.customGlyphs": false
  ```

  then *Developer: Reload Window* — the border glyphs are drawn from your
  font like normal text, which restores their colors (confirmed fix). If it
  is still wrong afterwards, also try
  `"terminal.integrated.gpuAcceleration": "off"`.
