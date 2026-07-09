---
title: Files, environment & policy
parent: Configuration
nav_section: Reference
nav_order: 6
---

# `env` and `setup` (experimental)

Council can export environment variables to agents and run commands before any
agent launches — a vendor-agnostic way to wire agents to a local service (a
context-compression proxy, a mock backend, a tunnel) without council knowing
anything about it. For a complete, runnable example, see
[examples/configs/headroom.yaml](https://github.com/umutarmut38/council/blob/main/examples/configs/headroom.yaml).

> **Experimental — off by default.** `setup` runs **arbitrary commands** and
> `env` mutates the agent environment, so the whole feature is opt-in. Set
> `experimental.setup_env: true` to turn it on in the merged effective config;
> otherwise any `env`/`setup` you configure is ignored and `council doctor`
> warns that it was. A trusted repo-local config can use `env`/`setup` when this
> flag is enabled globally.

```yaml
# Required: env/setup do nothing unless this is set.
experimental:
  setup_env: true

# exported to every agent process (merged under each agent's own env, which
# wins). Does NOT affect council's own subprocesses (git, gh).
env:
  OPENAI_BASE_URL: "http://127.0.0.1:8787"

# commands run once before agents launch (and re-run per one-shot CLI phase).
setup:
  - name: proxy                                   # optional label for logs/doctor
    command: ["headroom", "proxy", "--port", "8787"]
    background: true                              # supervised; stopped on exit
    wait_for_port: 8787                           # block until it's listening
  - command: ["docker", "compose", "up", "-d"]    # one-shot: run to completion
```

| Key | Meaning |
|---|---|
| `experimental.setup_env` | **Required to enable this feature.** `false` by default — `env`/`setup` are ignored unless this is `true`. |
| `env` | `KEY: value` map exported to every agent. Per-agent `agents.<name>.env` overrides it. |
| `setup[].command` | argv to run before launching agents. |
| `setup[].background` | `true` keeps the process alive for the session and terminates it on exit (a daemon/proxy). `false` (default) runs it to completion — a non-zero exit aborts startup. |
| `setup[].wait_for_port` | On a background command, block startup until `127.0.0.1:<port>` is listening (a readiness gate), up to ~10s. |
| `setup[].name` | Optional label shown in logs and `council doctor`. |

`council doctor` lists the exported env keys and setup commands and checks each
setup binary is on `PATH`. Setup runs once per interactive session and once per
`council run`; the standalone one-shot phases (`council plan`, etc.) each run it
for their own invocation.

> **Trust.** `setup` runs arbitrary commands, so from a **repo-local**
> `.council.yaml` it is gated exactly like the rest of the config: an untrusted
> or changed local file never runs setup or applies its env (`council trust` to
> approve, `--no-local-config` to ignore). Your global `~/.council.yaml` is
> always trusted.

See [`examples/configs/headroom.yaml`](https://github.com/umutarmut38/council/blob/main/examples/configs/headroom.yaml)
for routing agents through a local compression proxy.

---

# `files`

Limits for `@path` file-reference expansion in prompts and issues.

| Key | Default | Meaning |
|---|---|---|
| `allow_absolute` | `false` | By default only paths **inside the working directory** expand — a pasted task can't quietly inline `~/.ssh/id_rsa` into an agent prompt. Set `true` to allow absolute/outside paths (ignored under `policy.mode: safe`). |
| `max_bytes` | `262144` | Per-file size cap; bigger files stay as `@tokens`. Binary files are always skipped. |

---

# `policy`

```yaml
policy:
  mode: normal   # safe | normal | aggressive
```

| Mode | Behavior |
|---|---|
| `safe` | Refuses to run when enabled agents carry auto-approval flags; absolute `@file` refs never expand; destructive commands always confirm. |
| `normal` *(default)* | Doctor warns about risky flags; destructive commands ask for confirmation. |
| `aggressive` | Skips non-interactive adopt and clean confirmations — for sandboxed or fully-trusted environments. In-chat `/adopt` still confirms. |
