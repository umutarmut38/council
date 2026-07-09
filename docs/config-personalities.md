---
title: Personalities
parent: Configuration
nav_section: Reference
nav_order: 7
---

# `personalities` and `personality_categories`

Optional. **Behavioral** dispositions (how an agent thinks) that drive UI
grouping and prompt injection. They are independent of
[`role`](config-agents.md#roles), which is structural (who builds vs. who
judges).

```yaml
personality_categories:
  skeptical:
    label: Skeptical       # display label
    color: "203"           # optional 256-color code
    order: 20              # sort order within groupings

personalities:
  pessimist:
    label: Pessimist
    category: skeptical    # links to a category
    color: "203"
    order: 30
    prompt_prefix: |       # prepended to prompts sent to this agent
      Look for what can go wrong: risks, edge cases, missing tests…
```

Assign with `agents.<name>.personality: pessimist`. The `prompt_prefix` is
injected into broadcasts, `@agent`/`/send`, and orchestration prompts — but not
in direct mode. See [Workflows → Personalities](workflows.md#personalities-categories-and-targeting).
