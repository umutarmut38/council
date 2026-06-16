# Terminal Demo

A short terminal recording explains the **plan → vote → build → review → adopt**
flow faster than static diagrams. The recording is scripted with
[VHS](https://github.com/charmbracelet/vhs) so it stays reproducible: the script
is committed to the repo and anyone can regenerate the GIF from source.

<!--
Once the GIF has been recorded and committed, embed it here:

![council orchestration demo](assets/council-demo.gif)
-->

> The recorded GIF (`docs/assets/council-demo.gif`) is generated locally and is
> not committed, because it requires live agent CLIs to produce. Run the tape
> (below) to create it.

## What The Demo Shows

The tape drives the bundled retry/backoff example issue
(`examples/issues/retry-backoff.md`) through the full council workflow:

```text
/plan @examples/issues/retry-backoff.md
/vote
/build
/start-build
/review
/adopt
```

Each phase is given time for live agents to respond: workers draft plans,
reviewers rank them, the winning plan is built in isolated worktrees, the diffs
are reviewed, and the winner is adopted into the working tree.

## Regenerating The Recording

The tape lives at [`council-demo.tape`](assets/council-demo.tape) and writes its
output to `docs/assets/council-demo.gif`.

1. **Install VHS** — `brew install vhs`, or see the
   [VHS install guide](https://github.com/charmbracelet/vhs#installation).
2. **Put `council` on your PATH** — for example
   `go build -o bin/council ./cmd/council` and add `bin/` to `PATH`, or
   `brew install umutarmut38/council/council`.
3. **Configure agents** — enable at least two agents in `~/.council.yaml`, with
   both `worker` and `reviewer` roles present. The
   `examples/configs/worker-reviewer.yaml` starter config is a good base.
4. **Record from the repo root** so `@examples/...` resolves and orchestration
   has a git repository to work in:

   ```bash
   vhs docs/assets/council-demo.tape
   ```

This produces `docs/assets/council-demo.gif`. To preview without writing a file,
use `vhs --publish docs/assets/council-demo.tape`, or change the `Output` line in
the tape to `.mp4`/`.webm`/`.txt` for a different format.

The `Sleep` durations in the tape leave room for live agents to respond; tune
them to your machine and agents so each phase finishes before the next command
is typed.
