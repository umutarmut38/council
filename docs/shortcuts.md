# Keyboard shortcuts

Shortcuts depend on which screen/mode you're in. The default screen is the
**panes** view with the composer focused.

## Panes view (composer mode)

| Key | Action |
|---|---|
| `Enter` | Send the composer (or accept the highlighted file suggestion if open). |
| `↑` / `↓` | Navigate the command palette (after typing `/`) or @file suggestions. |
| `Tab` | Focus the next pane — or complete the `/command` you're typing. |
| `Shift+Tab` | Focus the previous pane. |
| `Ctrl+B` | Cycle the input target (all ↔ focused). |
| `Ctrl+F` | Zoom / un-zoom the focused pane (full screen). |
| `Ctrl+G` | Open the **overview** of all agents. |
| `Ctrl+N` | Next page of panes. |
| `Ctrl+P` | Previous page of panes. |
| `F2` or `Ctrl+O` | Enter **direct mode** (raw keystrokes to the focused pane). |
| `Ctrl+S` | Save transcripts to the run directory. |
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
