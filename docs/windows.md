---
title: Windows Support
nav_order: 10
---

# Windows Support

`council` runs natively on Windows using the Windows pseudo-console
(**ConPTY**) API. Windows is a **supported but still-maturing** target: the
Windows binary is built and shipped for every release and its compilation is
guarded by a *required* CI check, while the full runtime test suite on Windows
is allowed to fail. This page makes that stance explicit so you know what to
rely on and what to treat with caution.

## Support At A Glance

| Area | Status | How it is verified |
|---|---|---|
| Windows binary builds (amd64, arm64) | Supported — required | The `Windows cross-build smoke` CI job must pass on every change. |
| Release artifacts (amd64, arm64) | Shipped | The release workflow builds and publishes Windows `.zip` archives. |
| Full test suite on Windows | Experimental | `Test (windows-latest)` runs in CI but is `continue-on-error`. |
| Full-screen TUI rendering | Best-effort | Varies by console host; Windows Terminal is recommended. |

## What Is Supported

- **Native ConPTY agent panes.** Each agent runs in a real Windows
  pseudo-console, the same model used on macOS and Linux.
- **amd64 and arm64 binaries.** Both architectures are cross-compiled in CI and
  published as release archives.
- **npm-style agent shims.** Agent CLIs installed as `.cmd`/`.bat` shims (the
  usual shape of npm-installed tools such as `claude` or `codex`) are launched
  through the command interpreter automatically — no extra configuration needed.

## What Is Experimental

- **Runtime behavior is not yet gated by green tests.** The
  `Test (windows-latest)` job runs the full suite on Windows but is marked
  `continue-on-error`, so a Windows-only regression will *not* block a merge or
  release. Treat Windows as "works, but less battle-tested than macOS/Linux."
- Because of this, Windows-specific rendering and prompt-delivery issues are
  more likely to surface in day-to-day use. Please file them — see
  [Troubleshooting](troubleshooting.md#windows-specific-notes) first.

## What Is Only Smoke-Tested

- The **required** `Windows cross-build smoke` job only *cross-compiles* for
  `windows/amd64` and `windows/arm64` (`go build` to a discarded output). It
  guarantees the code still **compiles** for Windows so release binaries cannot
  silently rot. It does **not** execute the program or exercise runtime
  behavior.

## Recommended Terminal

- Use **[Windows Terminal](https://aka.ms/terminal)** for the best results.
  Modern terminal emulation handles full-screen TUIs, colors, and resizing far
  more gracefully than the legacy console.
- The legacy `conhost.exe` window handles full-screen TUIs less gracefully. If
  output looks garbled, switch to Windows Terminal or fall back to transcript
  rendering:

  ```yaml
  terminal:
    renderer: transcript
  ```

- **WSL** remains a fine alternative if you prefer a Linux environment; from
  WSL, `council` behaves like the Linux build.

## Known ConPTY Limitations

- **Minimum OS:** ConPTY requires Windows 10 1809 (build 17763) or newer; every
  supported release of Windows 10/11 qualifies. On older builds a child fails to
  start with `ConPty is not available` — use WSL instead.
- **Rendering varies by console.** Full-screen terminal behavior is less uniform
  than on Unix. `council`'s terminal emulation is pragmatic, not a complete
  terminal emulator, so a few agent UIs may render imperfectly in some hosts.
- **Some agent CLIs need delivery tweaks.** If an agent receives text but does
  not submit, or mangles multi-line prompts, configure the terminal delivery
  options (these are not Windows-specific but matter more here):

  ```yaml
  terminal:
    submit_sequence: cr
    submit_delay_ms: 250
    send_mode: paste
  ```

## How CI Treats Windows

The stance above is enforced by [`.github/workflows/ci.yml`](https://github.com/umutarmut38/council/blob/main/.github/workflows/ci.yml):

- **`Windows cross-build smoke` (required).** Runs on Linux and cross-compiles
  the project for `windows/amd64` and `windows/arm64`. This job must pass, so a
  change that breaks the Windows build is rejected.
- **`Test (windows-latest)` (allowed to fail).** Runs the full `go test ./...`
  suite on a real Windows runner with `continue-on-error: true`. It provides an
  early signal for Windows regressions without blocking the build while the
  Windows runtime story matures.

## See Also

- [Requirements](requirements.md) — platform support table and ConPTY notes.
- [Troubleshooting](troubleshooting.md#windows-specific-notes) — Windows-specific
  fixes for rendering and prompt delivery.
