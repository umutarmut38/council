---
title: Release Guide
nav_order: 11
---

# Release Guide

This guide prepares and publishes a GitHub release.

## Preflight

```bash
go test ./...
go vet ./...
go build ./...
git diff --check
```

Check the docs locally by reading `docs/index.md` or letting GitHub Pages build
from the `docs/` directory.

## Versioning

Use semantic versioning after the first tag:

- `v0.x.y` while APIs and workflows are still settling.
- `v1.0.0` once config compatibility and platform support are stable.

## Tagging

```bash
git tag -a v0.1.0 -m "council v0.1.0"
git push origin v0.1.0
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which:

- runs tests
- cross-builds macOS, Linux, and Windows artifacts
- injects version metadata
- uploads archives and checksums
- publishes a GitHub Release

## Homebrew Tap

Formulae live in the `umutarmut38/homebrew-council` tap, so users can:

```bash
brew install umutarmut38/council/council
```

On every `v*` release, the release workflow regenerates `Formula/council.rb`
from `packaging/homebrew/render_formula.sh` (using the release checksums) and
pushes it to the tap. This step needs a repository secret named
`HOMEBREW_TAP_TOKEN` — a token with `contents: write` on the tap repo. Without
it the step logs a skip and the release still succeeds; bump the formula
manually in that case:

```bash
gh release download vX.Y.Z --pattern checksums.txt
bash packaging/homebrew/render_formula.sh vX.Y.Z checksums.txt > Formula/council.rb
```

## Local Snapshot Build

```bash
make release-snapshot VERSION=v0.1.0
```

Artifacts are written under `dist/`.

## Release Checklist

- [ ] `CHANGELOG.md` has a dated release entry.
- [ ] README images render on GitHub.
- [ ] GitHub Pages workflow succeeds.
- [ ] CI is green on macOS, Linux, and Windows.
- [ ] Release artifacts contain `council version` metadata.
- [ ] Windows artifact is smoke-tested or clearly marked experimental.
- [ ] Internal planning notes are not tracked in the public release.

## Commit History

Before publishing, squash the feature branch into a clean release commit or a
small set of logical commits. Because this rewrites history, only do it after
all collaborators have pushed or saved their work.

One safe pattern:

```bash
git checkout main
git pull --ff-only
git checkout -b release/v0.1.0
git merge --squash session-navigation-personalities
git commit -m "Prepare council v0.1.0"
```

Then open a PR from `release/v0.1.0` to `main`.
