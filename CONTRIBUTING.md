# Contributing

Thanks for helping improve `council`.

## Development Setup

Requirements:

- Go 1.23+
- Git
- A terminal with ANSI color support

```bash
git clone https://github.com/umutarmut38/council
cd council
go test ./...
go build -o bin/council ./cmd/council
```

## Checks Before Opening a PR

```bash
go test ./...
go vet ./...
go build ./...
```

When changing terminal behavior, add tests where possible and manually verify at
least one real agent CLI. Terminal tools differ in how they accept pasted text,
Enter, resize events, and color sequences.

## Code Style

- Prefer small, focused changes.
- Keep agent-specific terminal quirks configurable instead of hard-coded by
  agent name.
- Preserve existing run artifacts and worktrees unless a command is explicitly
  meant to clean them.
- Use package-level docs for new internal packages and comments for non-obvious
  orchestration behavior.

## Release Process

See [docs/release.md](docs/release.md).
