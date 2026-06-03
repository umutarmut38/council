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
