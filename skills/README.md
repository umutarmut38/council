# Agent Skills

This directory holds [Agent Skills](https://github.com/agentskills/agentskills)
for AI coding CLIs. A skill is a folder with a `SKILL.md` file (YAML frontmatter
with `name` + `description`, then a Markdown body); the agent reads the
description to decide when to load the body. The format is an open standard
supported by Claude Code, OpenAI Codex CLI, Cursor, GitHub Copilot, and OpenCode.

This `skills/` directory is the **single source of truth** — the installer
copies a skill folder verbatim into each tool's native skills location. Same
skill content for every tool; no per-tool reformatting.

## `council-config`

[`council-config/`](council-config/SKILL.md) interactively scaffolds a
repo-local [`.council.yaml`](../docs/configuration.md) overlay: it interviews you
about which agent CLIs join the council, their roles, personalities, terminal
quirks, orchestration/auto-approval, the review gate, and UI, then writes the
file at the git root and excludes it locally via `.git/info/exclude` (never
`.gitignore`, never committed). Its
[`reference/schema.md`](council-config/reference/schema.md) mirrors the config
structs in `internal/config`.

## Install

The installer copies the skill into each tool's skills directory. By default it
installs **per-user** (available in every repo).

```bash
# Everything:
scripts/install-skill.sh --all

# Specific tools:
scripts/install-skill.sh --target claude,codex,cursor

# Project-scoped (into the current repo's per-project skills dirs):
scripts/install-skill.sh --all --scope project

# See where each target would land, or preview without writing:
scripts/install-skill.sh --list
scripts/install-skill.sh --all --dry-run

# Help:
scripts/install-skill.sh --help
```

Or via Make:

```bash
make install-skill                                   # --all
make install-skill TARGETS="--target claude,codex"   # subset
```

The script is idempotent: re-running leaves an up-to-date copy untouched, and
replaces a changed copy after moving the previous one to `<dir>.bak.<timestamp>`
(use `--force` to overwrite without a backup).

### Flags

| Flag | Meaning |
|---|---|
| `--all` | Install into every supported target. |
| `--target LIST` | Comma-separated subset of `claude,codex,cursor,copilot,opencode`. |
| `--scope user` | Per-user install, available in every repo (**default**). |
| `--scope project` | Per-project install under `--project-dir`. |
| `--project-dir DIR` | Project root for `--scope project` (default: current directory). |
| `--list` | Print each target's destination path and exit. |
| `--dry-run` | Print actions without writing. |
| `--force` | Overwrite existing skills without a backup. |
| `-h`, `--help` | Show help. |

## Per-tool skill locations

Each tool loads `SKILL.md` skills from its own directory. The installer writes a
`council-config/` folder inside these:

| Tool | User scope (default) | Project scope |
|---|---|---|
| Claude Code | `~/.claude/skills/` | `.claude/skills/` |
| OpenAI Codex CLI | `~/.codex/skills/` (`$CODEX_HOME`) | `.agents/skills/` |
| Cursor | `~/.cursor/skills/` | `.cursor/skills/` |
| GitHub Copilot | `~/.copilot/skills/` | `.github/skills/` |
| OpenCode | `~/.config/opencode/skills/` (`$XDG_CONFIG_HOME`) | `.opencode/skills/` |

Sources for these conventions:

- **Claude Code** — [Agent Skills docs](https://docs.anthropic.com/en/docs/claude-code/skills);
  personal skills live in `~/.claude/skills/<name>/SKILL.md`.
- **Codex CLI** — [openai/codex `docs/skills.md`](https://github.com/openai/codex/blob/main/docs/skills.md):
  `~/.codex/skills/**/SKILL.md`, behind the experimental flag (see note below).
- **Cursor** — Cursor loads skills from `.cursor/skills/<name>/SKILL.md` (project)
  and `~/.cursor/skills/` (user); each skill is a folder with a `SKILL.md`.
- **GitHub Copilot** — [GitHub Copilot now supports Agent Skills](https://github.blog/changelog/2025-12-18-github-copilot-now-supports-agent-skills/)
  and [Adding agent skills](https://docs.github.com/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/add-skills):
  personal skills in `~/.copilot/skills/`, project skills in `.github/skills/`.
- **OpenCode** — [Skills docs](https://opencode.ai/docs/skills/): global
  `~/.config/opencode/skills/<name>/SKILL.md`, project `.opencode/skills/`.

> **Codex needs the skills feature enabled.** Add the following to
> `~/.codex/config.toml` (or `$CODEX_HOME/config.toml`) and restart Codex:
>
> ```toml
> [features]
> skills = true
> ```

> **Cross-tool note.** Several tools also read each other's skill dirs — Codex,
> Copilot, and OpenCode all additionally discover `~/.claude/skills/` and/or
> `.agents/skills/`. Installing per tool (as above) keeps each one self-contained
> and avoids relying on that fallback.
