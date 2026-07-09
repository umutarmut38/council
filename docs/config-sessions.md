---
title: Sessions & review
parent: Configuration
nav_section: Reference
nav_order: 3
---

# `sessions`

| Key | Default | Meaning |
|---|---|---|
| `root_dir` | `.council/runs` | Where run directories are written. Relative paths are anchored to the directory council was launched from. |
| `private` | `true` | Run artifacts (raw logs, transcripts, prompts, diffs, check logs) are written owner-only: `0700` directories, `0600` files. Set `false` for shared-machine workflows that need group reads. |
| `redact` | `false` | Best-effort scrubbing of common secret patterns (AWS/GitHub/OpenAI/Slack keys, bearer tokens, PEM blocks, `api_key=` assignments) from **saved transcripts**. Raw PTY logs are a live stream and are not redacted — keep `private: true`. |

Old runs accumulate; prune them with `council clean-runs --keep 10`.

---

# `review`

| Key | Default | Meaning |
|---|---|---|
| `check_command` | empty | Run in each build worktree to gate implementations before the review vote; ones that fail (non-zero exit) are dropped. Empty = no gate (language-agnostic; every changed build goes to the vote). Set it per stack, e.g. `["go","build","./..."]`, `["npm","test"]`, `["cargo","build"]` — or let `council stack detect` write it for you. |
| `check_timeout_seconds` | `600` | Hard timeout per check run, so a hung test can't block review forever. A timeout is recorded as FAIL in the check log. |
| `max_check_output_bytes` | `1048576` | Cap on each check log; longer output is truncated. |
