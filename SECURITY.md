# Security Policy

`council` launches local coding-agent CLIs and can ask them to edit code, run
commands, and create git worktrees. Treat it as an automation tool with the same
trust level as the agents you configure.

## Supported Versions

Security fixes target the latest tagged release and `main`.

## Reporting a Vulnerability

Please open a private security advisory on GitHub or contact the maintainer
directly. Include:

- The affected version or commit.
- Operating system and terminal.
- Minimal reproduction steps.
- Whether the issue requires a malicious repository, malicious agent output, or
  only normal use.

## Operational Safety Notes

- Review local `.council.yaml` files before running in untrusted repositories.
- Be careful with agent commands that enable auto-approval or permission bypass.
- Build work runs in git worktrees, but `/adopt` applies a selected diff to your
  working tree as uncommitted changes. Review before committing.
- Raw logs and transcripts may contain prompt contents, local paths, secrets
  pasted into agents, and agent output. Do not publish `.council/runs/` blindly.
