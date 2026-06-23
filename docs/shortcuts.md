---
title: Keyboard Shortcuts
nav_order: 6
---

# Keyboard shortcuts

Shortcuts depend on which screen/mode you're in. The default screen is the
**panes** view with the composer focused.

## Mouse

Mouse support is on by default (`ui.mouse`); toggle it at runtime with `Ctrl+W`
(turning it off restores your terminal's native text selection / copy-paste),
or set `ui.mouse: false` to start with it off.

| Action | Effect |
|---|---|
| Wheel over a pane | Scroll that pane's history. A `↑N` marker on the border shows the pane isn't live; new output keeps accumulating below and the view stays put until you wheel back to the bottom. |
| Click a pane | Focus it. |
| Wheel on a list/pager screen | Scroll the selection or pager (overview, settings, runs, artifacts, compare). |

In **direct mode** and the **integrated editor**, mouse events are forwarded to
the agent's program when it has enabled mouse tracking (e.g. `nvim` with
`set mouse=a`, or `less`); otherwise the wheel falls back to scrolling council's
own pane history.

## Panes view (composer mode)

| Key | Action |
|---|---|
| `Enter` | Send the composer (or accept the highlighted file suggestion if open). |
| `↑` / `↓` | Navigate the command palette (after typing `/`) or @file suggestions. |
| `Tab` | Focus the next pane — or complete the `/command` you're typing. |
| `Shift+Tab` | Focus the previous pane. |
| `Ctrl+B` | Cycle the input target: all → each group → focused. Groups follow `ui.group_by` (personality or category); with `group_by: none` it's all ↔ focused. |
| `Ctrl+F` | Zoom / un-zoom the focused pane (full screen). |
| `Ctrl+G` | Open the **overview** of all agents. |
| `Ctrl+N` | Next page of panes. |
| `Ctrl+P` | Previous page of panes. |
| `F2` or `Ctrl+O` | Enter **direct mode** (raw keystrokes to the focused pane). |
| `Ctrl+S` | Save transcripts to the run directory. |
| `Ctrl+W` | Toggle mouse capture on/off (off restores native terminal text selection). |
| `Ctrl+U` | Clear the composer input. |
| `Ctrl+C` | Clear the input if non-empty; otherwise send `Ctrl+C` to the focused pane. |
| `Ctrl+D` | If the input is empty, send `Ctrl+D` (EOF) to the focused pane. |
| `Ctrl+X` | Quit (terminates the agent processes). |
| `Esc` | Close file suggestions if open; otherwise clear the input. |
| `Backspace` | Delete the last character. |
| printable keys | Type into the composer. |

### `@file` suggestions

Typing `@` (not at the very start as `@agent`) opens a file picker above the
input:

| Key | Action |
|---|---|
| keep typing | Filter, e.g. `@internal/tui`. |
| `Up` / `Down` | Move through suggestions. |
| `Enter` | Insert the highlighted file. |
| `Esc` | Close suggestions without changing the input. |
| `Tab` | **Not** used to accept — it stays reserved for switching panes. |

## Direct mode (`F2` / `Ctrl+O`)

All keystrokes are forwarded raw to the focused agent (so the agent's own
shortcuts work). Personality prompt prefixes are **not** injected in direct mode.

| Key | Action |
|---|---|
| any key | Sent to the focused pane. |
| `Esc`, `F2`, `Ctrl+O` | Return to composer mode. |
| `Ctrl+X` | Quit. |

## Overview (`Ctrl+G` / `/overview`)

A list of all agents grouped by the active grouping mode.

| Key | Action |
|---|---|
| `Up` / `Down` | Move the selection. |
| `Enter` | Focus the selected agent and jump to its page. |
| `Space` | Show/hide the selected agent's personality. |
| `Ctrl+N` / `Ctrl+P` | Next / previous page. |
| `Esc` | Back to the panes view. |

## Settings (`/settings`)

| Key | Action |
|---|---|
| `Up` / `Down` | Select a setting (e.g. page rows/cols, grouping). |
| `Left` / `Right` | Change the selected value. |
| `Esc` or `Enter` | Close the settings view. |

Settings changed here apply to the current session and are not written back to
YAML.

## Runs picker (`/runs`)

| Key | Action |
|---|---|
| `Up` / `Down` | Select a run. |
| `Enter` | Resume the selected run. |
| `Esc` | Back. |

## Integrated editor (`/edit`)

A VSCode-style split: a collapsible file tree on the left and the configured
editor (`ui.editor`, e.g. `nvim`) running in a PTY pane on the right.
`/edit <path>` opens a file immediately.

### File tree (left column, default focus)

| Key | Action |
|---|---|
| `Up` / `Down` (`k` / `j`) | Move the selection. |
| `→` / `Enter` / `l` | Expand a folder, or open the selected file in the editor pane. |
| `←` / `h` | Collapse the folder, or jump to its parent. |
| `g` / `Home`, `G` / `End` | Jump to the top / bottom. |
| `Tab` | Jump into the editor pane. |
| `Esc` | Leave the editor screen. |

### Editor pane (right column, after opening a file or `Tab`)

| Key | Action |
|---|---|
| any key, incl. `Esc` | Forwarded raw to the editor (so `Esc` leaves insert mode, `:q` quits, etc.). |
| `F2` / `Ctrl+O` | Return focus to the left column (the editor stays live). |
| `Ctrl+X` | Quit. |

Selecting another file opens it in the same live editor via `ui.editor_open_cmd`
(default `:e {path}`, tuned for vim/nvim; set it empty to relaunch the editor per
file for non-vim editors).

## Artifacts browser (`/artifacts`)

The same split, with the run's artifact list on the left instead of a file tree.
The editor-pane keys above apply on the right.

| Key | Action |
|---|---|
| `Up` / `Down` | Select an artifact. |
| `Enter` | Open the selected artifact in the editor pane (editable). |
| `Tab` | Jump into the editor pane (`F2` / `Ctrl+O` returns to the list). |
| `e` | Open the artifact in the external configured editor (`ui.editor`, else `$VISUAL` / `$EDITOR` / `vim`) instead. |
| `Esc` | Back to the panes view. |

Synthetic views — `/preview`, `/compare` diffs, and adopt previews — stay a
full-width read-only pager (`↑` / `↓` scroll, `Esc` back).
