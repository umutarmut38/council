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

## Tool Asks to Trust a Worktree Folder

Plan, vote, and review run in the launch directory. Build runs in per-agent git
worktrees. Some agent tools ask for trust in every new directory.

Options:

- Trust `.council/worktrees/<agent>` once in that tool.
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

This removes `.council/worktrees/<agent>` and the corresponding
`council/<agent>/<timestamp>` branches.
