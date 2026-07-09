---
title: UI & themes
parent: Configuration
nav_section: Reference
nav_order: 2
---

# `ui`

| Key | Default | Meaning |
|---|---|---|
| `layout` | `grid` | Pane layout. |
| `adaptive_grid` | `true` | Size the grid to the visible panes: 1 pane fills the screen, 2 sit side by side at full height, 3-4 use a 2x2. Larger rosters page with `page_rows` x `page_cols`. Adjusting rows/cols in `/settings` locks the layout for that session; set `false` to always use the configured grid. |
| `detect_approval_prompts` | `true` | **Experimental.** Auto-flag a pane as "needs input" when an approval-looking prompt sits at the bottom of its screen and the agent has been quiet for ~2s. Heuristic by nature — `/attention <agent>` is the manual, reliable path. Set `false` to disable. |
| `max_scrollback_lines` | `5000` | Per-pane scrollback kept in memory. |
| `initial_prompt_delay_ms` | `3000` | Wait this long after launch before broadcasting (lets agents finish booting). Raise it if agents miss the prompt — codex's MCP load is the slowest factor; `8000` is a good value when running many agents. |
| `page_rows`, `page_cols` | grid-derived | Panes per page (for many agents). |
| `group_by` | `none` | `none`, `personality`, or `category` — orders panes and the overview. |
| `theme` | `default` | Overall color palette — a built-in or a custom name from `themes`. See [Themes](#themes). |

## Themes

`ui.theme` recolors the whole UI chrome — header, footer, pane borders,
dividers, command suggestions, and the diff viewer. Four built-ins ship:

- `default` — the original palette.
- `nord` — cool: steel-blue brand, frost-cyan focus, slate borders.
- `solarized` — warm: yellow/orange brand, amber focus, teal rail.
- `mono` — high-contrast grayscale; red is reserved for warnings.

```yaml
ui:
  theme: nord
```

Per-agent and per-personality `color` settings still win for **pane borders**;
the theme drives everything else. `council doctor` prints its color test strip
in the active theme so you can preview it in your terminal.

Define your own palette under `ui.themes.<name>` and select it with `theme`.
Each role is an optional 256-color index (`"212"`) or hex (`"#ff5f87"`); any role
you omit inherits the `default` palette. Colors are indexed-256 only (no
truecolor), so they render identically across terminals.

```yaml
ui:
  theme: midnight
  themes:
    midnight:
      title: "117"
      focus: "213"
      warn: "203"
```

The full list of roles (`title`, `heading`, `status`, `rail`, `border`,
`focus`, `suggest`, `input`, `warn`, `faint`) is in the
[Schema reference](config-schema.md#uithemesname).
